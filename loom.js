
const ATTEMPTS_NUM = 100
const ATTEMPTS_WAIT = 39
const RECONNECT_INTERVAL = 10000
const METHOD_ECHO = "_echo"

export default class Loom {
  // resppnses recieved from backend
  responses = {}

  // handlers is a local methods allow to be handle callbacks
  handlers = {}

  _onclose = []
  _onopen = []
  _onreconnect = []

  addr = 'ws://...'

  constructor(addr) {
    const u = new URL(addr === '/' ? window.location.toString() : addr)

    // replace protocol http -> ws or https -> wss
    u.protocol = u.protocol === 'http:' ? 'ws:' : 'wss:'
    u.pathname = '/ws'
    u.search = ''

    this.addr = u.toString()
    this.init()
  }

  // initialize webscoket connection
  init() {
    this.socket = new WebSocket(this.addr)

    // recieve messages from backend
    this.socket.onmessage = (e) => {
      let msg = JSON.parse(e.data)

      if (msg.method === METHOD_ECHO) {
        return
      }

      if (msg.id === '0') {
        let h = this.handlers[msg.method]
        if (!h) {
          console.error(`handler ${msg.method} not found`)
          return
        }

        h.handler.call(h.ctx, msg.data)
        return
      }

      this.responses[msg.id] = {
        data: msg.data,
        err: msg.error,
      }
    }

    this.socket.onclose = (e) => {
      console.warn('close', e)

      // TODO: handler close by user
      setTimeout(() => {
        console.warn('reconnect...')
        this.init()
      }, RECONNECT_INTERVAL)

      for (let callback of this._onclose) {
        callback(this)
      }
    }

    this.socket.onerror = (e) => {
      console.error(e)
    }

    this.socket.onopen = (e) => {
      for (const callback of this._onopen) {
        callback(this)
      }
    }

    return this.socket
  }

  sethandler = (route, handler, ctx) => {
    let loom = this

    loom.handlers[route] = {
      handler: handler,
      ctx: ctx,
    }
  }

  _id() {
    return Math.random().toString(36).substr(2, 8)
  }

  _socket() {
    return new Promise((resolve, reject) => {
      let iter = 0
      let i = setInterval(() => {
        if (this.socket.readyState) {
          clearInterval(i)
          resolve(this.socket)
        }

        if (++iter > ATTEMPTS_NUM) reject('timeout')
      }, ATTEMPTS_WAIT)
    })
  }

  // call remote method and pass some data to it
  call(method, data) {
    let msg = {
      id: this._id(),
      method: method,
      data: data,
    }

    return this._socket().then((socket) => {
      socket.send(`${JSON.stringify(msg)}\n`)

      return new Promise((resolve, reject) => {
        let iter = 0
        let i = setInterval(() => {
          const resp = this.responses[msg.id]

          if (resp) {
            clearInterval(i)

            delete this.responses[msg.id]

            if (resp.err) reject(resp.err)
            else resolve(resp.data)
          }

          if (++iter > ATTEMPTS_NUM) reject('timeout')
        }, ATTEMPTS_WAIT)
      })
    })
  }

  onclose(method) {
    this._onclose.push(method)
  }

  onopen(method) {
    this._onopen.push(method)
  }

  onreconnect(method) {
    this._onreconnect.push(method)
  }
}
