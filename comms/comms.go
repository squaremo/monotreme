package comms

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dpw/monotreme/propagation"
	. "github.com/dpw/monotreme/rudiments"
)

type NodeDaemon struct {
	us       NodeID
	listener net.Listener

	lock         sync.Mutex
	connectivity *propagation.Connectivity
}

func NewNodeDaemon(bindAddr string) (*NodeDaemon, error) {
	us := NodeID(fmt.Sprint(time.Now().UnixNano())) // XXX

	l, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return nil, err
	}

	nd := &NodeDaemon{
		us:           us,
		listener:     l,
		connectivity: propagation.NewConnectivity(us),
	}

	go nd.acceptConnections()
	return nd, nil
}

func (nd *NodeDaemon) acceptConnections() {
	for {
		conn, err := nd.listener.Accept()
		if err != nil {
			// XXX
			log.Println(err)
			return
		}

		go nd.handleConnection(conn)
	}
}

func (nd *NodeDaemon) Connect(addr string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}

	go nd.handleConnection(conn)
	return nil
}

type connection struct {
	nd     *NodeDaemon
	conn   net.Conn
	closed int32
	cancel chan struct{}
	toSend chan struct{}

	// protected by the NodeDaemon lock
	connection *propagation.Connection
}

func (nd *NodeDaemon) handleConnection(conn net.Conn) {
	c := connection{
		nd:     nd,
		conn:   conn,
		cancel: make(chan struct{}),
		toSend: make(chan struct{}, 1),
	}

	go func() {
		err := c.writeSide()
		if c.close() && err != nil && err != io.EOF {
			log.Println(err)
		}
	}()

	err := c.readSide()
	if c.close() && err != nil && err != io.EOF {
		log.Println(err)
	}
}

func (c *connection) writeSide() error {
	w := newWriter(c.conn)

	writeNodeID(w, c.nd.us)
	if err := w.Flush(); err != nil {
		return err
	}

	for {
		select {
		case <-c.cancel:
			return nil
		case <-c.toSend:
		}

		if err := c.writePending(w); err != nil {
			return err
		}
	}
}

func (c *connection) writePending(w *writer) error {
	updates := c.connection.Outgoing()
	if updates != nil {
		func() {
			c.nd.lock.Lock()
			defer c.nd.lock.Unlock()
			writeConnectivityUpdates(w, updates)
		}()

		if err := w.Flush(); err != nil {
			return err
		}

		func() {
			c.nd.lock.Lock()
			defer c.nd.lock.Unlock()
			c.connection.Delivered(updates)
		}()
	}

	return nil
}

func (c *connection) readSide() error {
	r := newReader(c.conn)

	them := readNodeID(r)
	if r.err != nil {
		return r.err
	}

	func() {
		c.nd.lock.Lock()
		defer c.nd.lock.Unlock()
		c.connection = c.nd.connectivity.Connect(them)
		c.connection.SetPendingFunc(func() {
			select {
			case c.toSend <- struct{}{}:
			}
		})
	}()

	for {
		updates := readConnectivityUpdates(r)
		if r.err != nil {
			return r.err
		}

		func() {
			c.nd.lock.Lock()
			defer c.nd.lock.Unlock()
			c.connection.Receive(updates)
			log.Println(c.nd.connectivity.Dump())
		}()
	}
}

func (c *connection) close() bool {
	if atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		c.conn.Close()
		close(c.cancel)

		c.nd.lock.Lock()
		defer c.nd.lock.Unlock()
		c.connection.Close()

		return true
	}

	return false
}
