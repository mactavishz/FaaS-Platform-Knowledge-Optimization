// Tree-C: Intermediate function with CPU work, calls F/G async
// Express-style handler (req, res)

import axios from "axios";
import { Worker } from "node:worker_threads";

const GATEWAY_BASE = process.env.TINYFAAS_GATEWAY_URL || "http://tinyfaas.com";

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
    return (async () => {
        const pathPrefix = sync ? "/fn/" : "/async-fn/";
        const url = new URL(`${GATEWAY_BASE}${pathPrefix}tree-${functionName}`);

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
            console.error(
                `Error calling ${functionName}:`,
                err && err.message ? err.message : err,
            );
            throw err;
        }
    })();
}

export default async (req, res) => {
    console.log("Event for C:", req.body);

    let calls = [];
    calls.push(callFunction("f", { test: "event" }, false, req.headers));
    calls.push(callFunction("g", { test: "event" }, false, req.headers));

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

    let results = await Promise.all(calls);

    console.log("Results are", results);

    res.json({
        results: results,
        w: [r1, r2],
    });
};
