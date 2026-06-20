'use strict';

// Tree-C: Intermediate function with CPU work, calls F/G async
// Express-style handler (req, res)

const axios = require("axios");
const { Worker } = require("node:worker_threads");

const GATEWAY_BASE = (process.env.FAASD_GATEWAY_URL || "http://127.0.0.1:8080").replace(/\/$/, "");

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
        const suffix = sync ? "function" : "async-function";
        const url = new URL(`${GATEWAY_BASE}/${suffix}/tree-${functionName}`);

        const headers = Object.assign({}, incomingHeaders || {});
        delete headers.host;
        delete headers["content-length"];
        headers["content-type"] = "application/json";
        // Mark async hops so the provider can record the call-graph edge kind.
        // Set only when async; deleted on sync so an inherited value cannot leak
        // into a downstream sync call.
        if (!sync) {
            headers["x-faas-async"] = "true";
        } else {
            delete headers["x-faas-async"];
        }

        try {
            const res = await axios.post(url.toString(), data, {
                headers,
                timeout: 60000,
                validateStatus: () => true,
            });

            if (!sync && res.status !== 202) {
                throw new Error(`Async call failed with status ${res.status}`);
            }

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

module.exports = async (request, context) => {
    console.log("Event for C:", request.body);

    let calls = [];
    calls.push(callFunction("f", { test: "event" }, false, request.headers));
    calls.push(callFunction("g", { test: "event" }, false, request.headers));

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

    let results = await Promise.all(calls);

    console.log("Results are", results);

    return context.status(200).succeed({
        results: results,
        w: [r1, r2],
    });
};
