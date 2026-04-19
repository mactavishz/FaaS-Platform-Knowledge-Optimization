'use strict';

// Webshop-email: Email - simulates sending a confirmation email with CPU load (worker thread)
// Express-style handler (req, res)

const { Worker } = require("worker_threads");

// Worker thread code (CommonJS, evaluated inline)
const WORKER_CODE = `
const { workerData, parentPort } = require('worker_threads');

let num = workerData.num || 7;
let res = cpu_intensive(num);
parentPort.postMessage(res);

// https://gist.github.com/sqren/5083d73f184acae0c5b7
function cpu_intensive(baseNumber) {
    let result = 0;
    for (var i = Math.pow(baseNumber, 7); i >= 0; i--) {
        result += Math.atan(i) * Math.tan(i);
    }
    return result;
}
`;

function spawnWorker() {
    return new Promise((resolve, reject) => {
        const worker = new Worker(WORKER_CODE, { workerData: {}, eval: true });
        worker.on("message", (m) => resolve(m));
        worker.on("error", (m) => reject(m));
    });
}

module.exports = async (request, context) => {
    const event = request.body;
    console.log("Email: Event:", event);

    // Simulate I/O latency with CPU-bound work in a separate thread
    await spawnWorker();

    return context.status(200).succeed({ success: Math.random() > 0.1 });
};
