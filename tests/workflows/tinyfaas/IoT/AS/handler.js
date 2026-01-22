// IoT-AS: ActionSignage - CPU work + simulated DynamoDB batch writes
// Express-style handler (req, res)
// DynamoDB calls replaced with simulated latency

const { Worker } = require("worker_threads");

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Simulated DynamoDB batch write latency (configurable via env)
// Original: writes one item per sensor in a loop
const DDB_PUT_MS = parseInt(process.env.IOT_DDB_PUT_MS) || 25;

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

module.exports = async (req, res) => {
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
    let val1 = parseInt(event.location) || 0;
    let val2 = (parseInt(event.location) || 0) + (parseInt(event.chain) || 1);
    let startLoc = Math.min(val1, val2);
    let endLoc = Math.max(val1, val2);
    let itemCount = endLoc - startLoc + 1;

    console.log("Setting Sensor from", startLoc, "to", endLoc, `(${itemCount} items)`);

    // Original: loop of ddb.putItem calls to UseCaseTable
    // Simulating batch DynamoDB writes (total latency scales with item count)
    let batchLatency = DDB_PUT_MS * itemCount;
    console.log(`Simulating DynamoDB batch writes (${batchLatency}ms for ${itemCount} items)...`);
    
    // Wait for CPU work and simulated DB writes in parallel
    await Promise.all([w1, w2, sleep(batchLatency)]);

    res.json({
        from: "ActionSignage",
        simulated: true,
        itemCount: itemCount,
    });
};
