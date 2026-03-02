// Tree-F: Leaf function with CPU work
// Express-style handler (req, res)

import { Worker } from "node:worker_threads";

const js_string = `
const { workerData, parentPort } = require('worker_threads');

let num = workerData.num || 7;
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

export default async (req, res) => {
    console.log("Event for F:", req.body);
    let start = Date.now();

    let num = req.body?.num ?? 7;

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

    console.log(`F finished CPU work in ${Date.now() - start}ms`);

    res.json([r1, r2]);
};
