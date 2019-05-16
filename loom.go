package loom

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"sync"

	"golang.org/x/net/websocket"
)

var Debug bool

type Handler func(req interface{}) (resp interface{}, err error)

type Loom struct {
	clients  sync.Map // key: websocket.Conn, value: *client
	handlers sync.Map // key: route path,     value: *handler

	onConnect    Handler
	onDisconnect Handler
}

//
// initialize
//

// NewLoom return instance of Loom
func NewLoom() *Loom {
	return &Loom{}
}

//Handler return http.Handler with websocket
func (l *Loom) Handler() http.Handler {
	return websocket.Handler(l.wshandler)
}

// SetHandler by route path
func (l *Loom) SetHandler(route string, h interface{}) {
	l.handlers.Store(route, newhandler(h))
}

// OnConnect bind event onconnect
func (l *Loom) OnConnect(f Handler) {
	l.onConnect = f
}

// OnDisconnect bind event onDisconnect
func (l *Loom) OnDisconnect(f Handler) {
	l.onDisconnect = f
}

//
// handler
//

type handler struct {
	h interface{}
	v reflect.Value
	t reflect.Type

	req reflect.Type
}

func newhandler(h interface{}) *handler {
	v := reflect.ValueOf(h)
	t := reflect.TypeOf(h)
	req := t.In(0).Elem()

	return &handler{h: h, v: v, t: t, req: req}
}

// gethandler by route path
func (l *Loom) gethandler(route string) (*handler, bool) {
	h, ok := l.handlers.Load(route)
	if !ok {
		return nil, ok
	}

	return h.(*handler), ok
}

func (h *handler) call(data []byte) (resp []byte, err error) {
	req := reflect.New(h.req)
	if err := json.Unmarshal(data, req.Interface()); err != nil {
		return nil, err
	}

	out := h.v.Call([]reflect.Value{req})

	if !out[0].IsNil() {
		resp, _ = json.Marshal(out[0].Interface())
	}
	if !out[1].IsNil() {
		err = out[1].Interface().(error)
	}

	return
}

//
// client
//

type client struct {
	ws     *websocket.Conn
	closed bool
}

//wshandler is handler for websocket connections
func (l *Loom) wshandler(ws *websocket.Conn) {
	c := l.getclient(ws)
	if Debug {
		log.Println("new client:", c.ws.RemoteAddr())
	}

	scanner := bufio.NewScanner(ws)
	for scanner.Scan() && c != nil {
		req, err := l.parseRequest(scanner.Bytes())
		if err != nil {
			log.Println(err)
			continue
		}

		go func(req request) {
			resp, err := req.handler.call(req.Data)
			c.write(req.ID, resp, err)
		}(req)
	}

	l.disconnect(c)
}

func (l *Loom) getclient(ws *websocket.Conn) *client {
	if v, ok := l.clients.Load(ws); ok {
		return v.(*client)
	}

	c := &client{ws: ws}
	l.clients.Store(ws, c)

	return c
}

func (c *client) write(id string, resp []byte, err error) {
	if c.closed {
		return
	}

	if err := json.NewEncoder(c.ws).Encode(response{
		ID:    id,
		Data:  string(resp),
		Error: err,
	}); err != nil {
		log.Println("ERROR", err)
	}
}

func (l *Loom) disconnect(c *client) {
	l.clients.Delete(c.ws)
	c.ws.Close()
	c.closed = true
}

//
// request
//

type request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Data   json.RawMessage `json:"data"`

	handler *handler
}

func (l *Loom) parseRequest(data []byte) (req request, err error) {
	if len(data) == 0 {
		return req, errors.New("request is empty")
	}

	if err := json.Unmarshal(data, &req); err != nil {
		return req, err
	}

	h, ok := l.gethandler(req.Method)
	if !ok {
		return req, fmt.Errorf("handler '%s' not found", req.Method)
	}
	req.handler = h

	return
}

//
// response
//

type response struct {
	ID    string `json:"id"`
	Data  string `json:"data"`
	Error error  `json:"error"`
}
