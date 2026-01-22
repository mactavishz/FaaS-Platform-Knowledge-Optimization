// IoT-DJ: DetectJam - CPU work + simulated DynamoDB write
// Express-style handler (req, res)
// DynamoDB calls replaced with simulated latency

const { Worker } = require("worker_threads");

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Simulated DynamoDB PutItem latency (configurable via env)
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
    console.log("DetectJam: Event:", event);

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

    // Original: ddb.putItem to UseCaseTable with SensorID=998
    // Simulating DynamoDB PutItem latency
    console.log(`Simulating DynamoDB PutItem (${DDB_PUT_MS}ms)...`);
    
    // Wait for CPU work and simulated DB write in parallel
    await Promise.all([w1, w2, sleep(DDB_PUT_MS)]);

    res.json({
        from: "DetectJam",
        simulated: true,
    });
};