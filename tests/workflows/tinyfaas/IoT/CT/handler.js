// IoT-CT: CheckTemperature - CPU load then calls AS async
// Express-style handler (req, res)

import axios from "axios";
import { Worker } from "node:worker_threads";

const GATEWAY_BASE = process.env.TINYFAAS_GATEWAY_URL || "http://tinyfaas.com";

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

function callFunction(functionName, data, sync, incomingHeaders) {
    return (async () => {
        const pathPrefix = sync ? "/fn/" : "/async-fn/";
        const url = new URL(`${GATEWAY_BASE}${pathPrefix}iot-${functionName}`);

        const headers = Object.assign({}, incomingHeaders || {});
        delete headers.host;
        delete headers["content-length"];
        headers["content-type"] = "application/json";

        try {
            const res = await axios.post(url.toString(), data, {
                headers,
                timeout: 60000,
                maxRedirects: 0,
                validateStatus: () => true,
            });

            if (!sync) {
                return {};
            }

            try {
                if (res.data === undefined || res.data === null || res.data === "") {
                    return {};
                }
                if (typeof res.data === "object") {
                    return res.data;
                }
                return JSON.parse(res.data);
            } catch {
                return {};
            }
        } catch (err) {
            console.error(`Error calling ${functionName}:`, err && err.message ? err.message : err);
            throw err;
        }
    })();
}

export default async (req, res) => {
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
    
    await w1;
    await w2;
    
    let sensorID = event.originalEvent ? event.originalEvent.sensorID : 0;
    let chain = event.originalEvent ? event.originalEvent.chain : 1;

    // Async call to as (ActionSignage)
    let actionSignage = await callFunction("as", { location: sensorID, chain: chain }, false, req.headers);

    res.json({
        from: "CheckTemperature",
        actionSignage: actionSignage,
    });
};
