'use strict';

class Loom {
	constructor(addr, onopen) {
		let loom = this;
		loom.responses = {};
		loom.handlers = {};

		loom.socket = new WebSocket(addr);

		loom.socket.onmessage = function(e) {
			let msg = JSON.parse(e.data);

			if (msg.id == "0") {
				let h = loom.handlers[msg.method];
				if (!h) {
					console.error(`handler ${msg.method} not found`);
					return;
				}

				h.handler.call(h.ctx, msg.data);
				return;
			}

			loom.responses[msg.id] = {
				data: msg.data,
				err: msg.error,
			}
		};

		loom.socket.onclose = function(e) {
			console.log("close", e);
		};

		loom.socket.onerror = function(e) {
			console.error(e);
		};

		loom.socket.onopen = function() {
			if (onopen) onopen(loom);
		}
	}

	sethandler(route, handler, ctx) {
		let loom = this;

		// console.log(handler);
		// console.log(ctx);

		// handler.bind(ctx);
		loom.handlers[route] = {
			handler: handler,
			ctx: ctx
		};
	}

	_id() {
		return Math.random().toString(36).substr(2, 8);
	}

	_socket() {
		let loom = this;

		return new Promise(function(resolve, reject) {
			let iter = 0;
			let i = setInterval(function() {

				if (loom.socket.readyState) {
					clearInterval(i);
					resolve(loom.socket);
				}

				if (++iter > 100) reject("timeout");
			}, 50);
		});
	}

	call(method, data) {
		let loom = this;

		let msg = {
			id: loom._id(),
			method: method,
			data: data,
		};

		return loom._socket().then(function(socket) {
			socket.send(`${JSON.stringify(msg)}\n`);

			return new Promise(function(resolve, reject) {
				let iter = 0;
				let i = setInterval(function() {
					let resp = loom.responses[msg.id];

					if (resp) {
						clearInterval(i);

						if (resp.err) reject(resp.err);
						resolve(resp.data);
					}

					if (++iter > 100) reject("timeout");
				}, 50);
			});
		})
	}
}