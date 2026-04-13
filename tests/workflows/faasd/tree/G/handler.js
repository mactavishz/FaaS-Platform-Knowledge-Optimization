'use strict';

// Tree-G: Leaf function with CPU work
// Express-style handler (req, res)

const { Worker } = require("node:worker_threads");

const js_string = `
const { workerData, parentPort } = require('worker_threads');

let num = workerData.num || 8.8;
let res = cpu_intensive(num);

parentPort.postMessage(res);

function cpu_intensive(baseNumber) {
    let result = 0;
    for (var i = Math.pow(baseNumber, 7); i >= 0; i--) {
        result += Math.atan(i) * Math.tan(i);
    }
    return result;
}
`;

module.exports = async (request, context) => {
    console.log("Event for G:", request.body);
    let start = Date.now();

    let num = request.body?.num ?? 7;

    let w1 = new Promise((resolve, reject) => {
        const worker = new Worker(js_string, {
            workerData: { num },
            eval: true,
        });
        worker.on("message", (m) => resolve(m));
        worker.on("error", (m) => reject(m));
    });
    let w2 = new Promise((resolve, reject) => {
        const worker = new Worker(js_string, {
            workerData: { num },
            eval: true,
        });
        worker.on("message", (m) => resolve(m));
        worker.on("error", (m) => reject(m));
    });

    let r1 = await w1;
    let r2 = await w2;

    console.log(`G finished CPU work in ${Date.now() - start}ms`);
    return context.status(200).succeed([r1, r2]);
};
