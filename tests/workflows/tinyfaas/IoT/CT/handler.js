// IoT-CT: CheckTemperature - CPU load then calls AS async
// Express-style handler (req, res)

const http = require("http");
const { Worker } = require("worker_threads");

const GATEWAY_BASE = "http://tinyfaas.com";

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

function callFunction(functionName, data, sync, incomingHeaders) {
    return new Promise((resolve, reject) => {
        const url = new URL(`${GATEWAY_BASE}/fn/iot-${functionName}`);
        const postData = JSON.stringify(data);
        
        // Start with all incoming headers to preserve tracing headers
        const headers = Object.assign({}, incomingHeaders || {});
        
        // Overwrite necessary fields
        headers["Content-Type"] = "application/json";
        headers["Content-Length"] = Buffer.byteLength(postData);
        
        // Remove host header to avoid conflicts
        delete headers["host"];
        delete headers["content-length"]; // Remove lowercase version
        
        // For async calls, set the async header
        if (!sync) {
            headers["X-Tinyfaas-Async"] = "true";
        } else {
            delete headers["x-tinyfaas-async"];
        }
        
        const options = {
            hostname: url.hostname,
            port: url.port || 80,
            path: url.pathname,
            method: "POST",
            headers: headers,
        };
        
        const req = http.request(options, (res) => {
            let body = "";
            res.on("data", (chunk) => { body += chunk; });
            res.on("end", () => {
                if (!sync) {
                    resolve({});
                } else {
                    try {
                        resolve(body ? JSON.parse(body) : {});
                    } catch (e) {
                        resolve({});
                    }
                }
            });
        });
        
        req.on("error", (e) => {
            console.error(`Error calling ${functionName}:`, e.message);
            reject(e);
        });
        
        req.write(postData);
        req.end();
    });
}

module.exports = async (req, res) => {
    const event = req.body;
    console.log("CheckTemperature: Event:", event);

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
    
    await Promise.all([w1, w2]);

    let sensorID = event.originalEvent ? event.originalEvent.sensorID : 0;
    let chain = event.originalEvent ? event.originalEvent.chain : 1;

    // Async call to as (ActionSignage)
    let actionSignage = await callFunction("as", { location: sensorID, chain: chain }, false, req.headers);

    res.json({
        from: "CheckTemperature",
        actionSignage: actionSignage,
    });
};