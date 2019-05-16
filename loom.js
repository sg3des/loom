'use strict';

class Loom {
	constructor(addr, onopen) {
		let loom = this;
		loom.responses = {};

		loom.socket = new WebSocket(addr);

		loom.socket.onmessage = function(e) {
			let msg = JSON.parse(e.data);

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
			onopen(loom);
		}
	}

	_id() {
		return Math.random().toString(36).substr(2, 8);
	}

	call(method, data) {
		let loom = this;

		let msg = {
			id: loom._id(),
			method: method,
			data: data,
		};

		loom.socket.send(`${JSON.stringify(msg)}\n`);

		return new Promise(function(resolve, reject) {
			let i = setInterval(function() {
				let resp = loom.responses[msg.id];

				if (resp) {
					clearInterval(i);

					if (resp.err) reject(resp.err);
					resolve(JSON.parse(resp.data));
				}

			}, 50);
		});
	}
}