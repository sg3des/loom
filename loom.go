package loom

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/op/go-logging"
	"golang.org/x/net/websocket"
)

var log = logging.MustGetLogger("LOOM")
var Debug bool

type Handler func(req interface{}) (resp interface{}, err error)
type ClientHandler func(*Client)

const clientFieldName = "Client"

type Loom struct {
	clients  sync.Map
	handlers map[string]*handler

	onConnect    ClientHandler
	onDisconnect ClientHandler
}

//
// initialize
//

// NewLoom return instance of Loom
func NewLoom() *Loom {
	return &Loom{
		handlers: make(map[string]*handler),
	}
}

// Handler return http.Handler with websocket
func (l *Loom) Handler() http.Handler {
	return websocket.Handler(l.wshandler)
}

type HTTPconn struct {
	http.Handler
	sync.Map
}

// SetHandler by route path
func (l *Loom) SetHandler(route string, h interface{}) {
	l.handlers[route] = newhandler(h)
}

// OnConnect bind event onconnect
func (l *Loom) OnConnect(f ClientHandler) {
	l.onConnect = f
}

// OnDisconnect bind event onDisconnect
func (l *Loom) OnDisconnect(f ClientHandler) {
	l.onDisconnect = f
}

//
// handler
//

type handler struct {
	h interface{}
	v reflect.Value
	t reflect.Type

	req        reflect.Type
	passclient bool
}

func newhandler(h interface{}) *handler {
	handler := &handler{
		h: h,
		v: reflect.ValueOf(h),
		t: reflect.TypeOf(h),
	}

	handler.req = handler.t.In(0).Elem()
	_, handler.passclient = handler.req.FieldByName(clientFieldName)

	return handler
}

// gethandler by route path
func (l *Loom) gethandler(route string) (h *handler, ok bool) {
	h, ok = l.handlers[route]
	return
}

func (h *handler) call(data json.RawMessage, c *Client) (resp interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()

	req := reflect.New(h.req)
	if err := json.Unmarshal(data, req.Interface()); err != nil {
		return resp, err
	}

	if h.passclient {
		req.Elem().FieldByName(clientFieldName).Set(reflect.ValueOf(c))
	}

	out := h.v.Call([]reflect.Value{req})

	switch len(out) {
	case 1:
		if !out[0].IsNil() {
			r := out[0].Interface()
			if e, ok := r.(error); ok {
				err = e
			} else {
				resp = r
			}
		}
	case 2:
		if !out[0].IsNil() {
			resp = out[0].Interface()
		}
		if !out[1].IsNil() {
			err = out[1].Interface().(error)
		}
	}

	return
}

//
// client
//

type Client struct {
	ws     *websocket.Conn
	closed bool

	sendMsgs  chan string
	closeChan chan interface{}

	allowBroadcast bool

	Request *http.Request
}

// wshandler is handler for websocket connections
func (l *Loom) wshandler(ws *websocket.Conn) {
	c := l.newClient(ws)
	if Debug {
		log.Debug("new client:", c.ws.RemoteAddr())
		log.Debug("total clients:", l.ClientsLen())
	}

	if l.onConnect != nil {
		l.onConnect(c)
		log.Debug("total clients:", l.ClientsLen())
	}

	scanner := bufio.NewScanner(ws)
	for scanner.Scan() && c != nil && !c.closed {
		msg, err := l.parsemsg(scanner.Bytes())
		if err != nil {
			log.Error(err)
			continue
		}

		if Debug {
			log.Debug("call:", msg.Method, string(msg.Data))
			log.Debug("total clients:", l.ClientsLen())
		}

		go func(msg *message) {
			resp, err := msg.handler.call(msg.Data, c)
			if Debug {
				log.Debug("resp:", resp, err)
			}

			rawmsg := newmsg(msg.ID, "", resp, err)
			l.sendmsg(c, rawmsg)
		}(msg)
	}

	if l.onDisconnect != nil {
		l.onDisconnect(c)
	}

	l.Disconnect(c)
}

func (l *Loom) newClient(ws *websocket.Conn) *Client {
	if c, ok := l.clients.Load(ws); ok {
		return c.(*Client)
	}

	req := ws.Request()
	c := &Client{
		ws:             ws,
		sendMsgs:       make(chan string, 128),
		closeChan:      make(chan interface{}),
		allowBroadcast: req.URL.Query().Get("broadcast") != "false",
		Request:        req,
	}

	go l.sendMsgsListener(c)
	go l.sendEchoMessages(c)

	l.clients.Store(ws, c)

	return c
}

func (l *Loom) Disconnect(c *Client) {
	if Debug {
		log.Debug("disconnect client:", c.ws.RemoteAddr())
	}
	if c.closed {
		log.Warning("attempt to disconnect already disconnected client")
		return
	}

	c.closed = true
	l.clients.Delete(c.ws)

	c.closeChan <- nil
}

//
// message
//

type message struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Data   json.RawMessage `json:"data"`
	Error  string          `json:"error"`

	handler *handler
}

func newmsg(id, method string, data interface{}, errmsg error) (resp string) {
	defer func() {
		if r := recover(); r != nil {
			log.Critical(r)
			log.Criticalf("%+v", data)
			resp = "{}"
		}
	}()

	jsondata, err := json.Marshal(data)
	if err != nil {
		log.Errorf("%+v", data)
		log.Error(string(jsondata))
		log.Critical(err)
		jsondata = []byte("{}")
	}

	msg := message{
		ID:     id,
		Method: method,
		Data:   jsondata,
	}

	if errmsg != nil {
		msg.Error = errmsg.Error()
	}

	b, _ := json.Marshal(msg)

	return string(b)
}

func (l *Loom) parsemsg(data []byte) (msg *message, err error) {
	if len(data) == 0 {
		return nil, errors.New("request is empty")
	}

	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	h, ok := l.gethandler(msg.Method)
	if !ok {
		return msg, fmt.Errorf("handler '%s' not found", msg.Method)
	}
	msg.handler = h

	return
}

var ErrClientClosed = errors.New("connection closed")

func (l *Loom) sendmsg(c *Client, rawmsg string) {
	select {
	case c.sendMsgs <- rawmsg:
	default:
		if Debug {
			log.Warning("channel is full")
		}
	}
}

func (l *Loom) sendMsgsListener(c *Client) {
	for !c.closed {
		var rawmsg string
		select {
		case rawmsg = <-c.sendMsgs:
			// continue
		case <-c.closeChan:
			return
		}

		if c.closed {
			break
		}

		_, err := fmt.Fprintln(c.ws, rawmsg)
		if err != nil && err != ErrClientClosed {
			l.Disconnect(c)
			return
		}
	}
}

func (l *Loom) sendEchoMessages(c *Client) {
	for !c.closed {
		time.Sleep(1 * time.Minute)

		if c.closed {
			return
		}
		if err := c.Call("_echo", nil); err != nil {
			l.Disconnect(c)
		}
	}
}

func (c *Client) Call(method string, data interface{}) error {
	if c.closed {
		return ErrClientClosed
	}

	rawmsg := newmsg(remoteCallID, method, data, nil)

	_, err := fmt.Fprintln(c.ws, rawmsg)
	return err
}

func (c *Client) Connected() bool {
	return !c.closed
}

func (c *Client) IP() string {
	return c.ws.RemoteAddr().String()
}

//
// broadcast
//

const remoteCallID = "0"

func (l *Loom) Broadcast(method string, data interface{}) {
	rawmsg := newmsg(remoteCallID, method, data, nil)

	l.clients.Range(func(key, val interface{}) bool {
		c := val.(*Client)

		if c.allowBroadcast {
			l.sendmsg(val.(*Client), rawmsg)
		}

		return true
	})
}

func (l *Loom) ClientsLen() (n int) {
	l.clients.Range(func(key, val interface{}) bool {
		n++
		return true
	})
	return
}
