// IoT-AS: ActionSignage - CPU work + simulated DynamoDB batch writes
// Express-style handler (req, res)
// DynamoDB calls replaced with simulated latency

import { Worker } from "node:worker_threads";

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Simulated DynamoDB batch write latency (configurable via env)
// Original: writes one item per sensor in a loop
const DDB_PUT_MS = parseInt(process.env.IOT_DDB_PUT_MS) || 25;

const js_string = `
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

export default async (req, res) => {
    const event = req.body;
    console.log("ActionSignage: Event:", event);

    // CPU-intensive work via worker threads
    let w1 = new Promise((resolve, reject) => {
        const worker = new Worker(js_string, { workerData: {}, eval: true });
        worker.on("message", (m) => resolve(m));
        worker.on("error", (m) => reject(m));
    });
    let w2 = new Promise((resolve, reject) => {
        const worker = new Worker(js_string, { workerData: {}, eval: true });
        worker.on("message", (m) => resolve(m));
        worker.on("error", (m) => reject(m));
    });

    // Calculate range for batch writes
    let promises = []
    let val1 = parseInt(event.location) || 0;
    let val2 = (parseInt(event.location) || 0) + (parseInt(event.chain) || 1);
    let startLoc = Math.min(val1, val2);
    let endLoc = Math.max(val1, val2);
    let itemCount = endLoc - startLoc + 1;

    console.log("Setting Sensor from", startLoc, "to", endLoc, `(${itemCount} items)`);
    for (let currId = startLoc; currId <= endLoc; currId++) {
        // let params = {
        //     TableName: "UseCaseTable",
        //     Item : {
        //         'SensorID': {N: currId + ''},
        //         'Message': {S: JSON.stringify(event)}
        //     }
        // }
        try {
            let response = await sleep(DDB_PUT_MS);
            promises.push(response)
        } catch (error) {
            console.log(error)
            await new Promise(resolve => setTimeout(resolve, 100)) // Sleep 100ms if this doesnt work
        }
    }
    await w1
    await w2
    let answers = await Promise.all(promises) 

    res.json({
        from: "ActionSignage",
        simulated: true,
        itemCount: answers,
    });
};
