'use strict';

// IoT-CS: CheckSound - calls CSL and CSA in parallel (sync)
// Express-style handler (req, res)

const axios = require("axios");

const GATEWAY_BASE = (process.env.FAASD_GATEWAY_URL || "http://127.0.0.1:8080").replace(/\/$/, "");

function callFunction(functionName, data, sync, incomingHeaders) {
    return (async () => {
        const suffix = sync ? "function" : "async-function";
        const url = new URL(`${GATEWAY_BASE}/${suffix}/iot-${functionName}`);

        const headers = Object.assign({}, incomingHeaders || {});
        delete headers.host;
        delete headers["content-length"];
        headers["content-type"] = "application/json";

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
            console.error(`Error calling ${functionName}:`, err && err.message ? err.message : err);
            throw err;
        }
    })();
}

module.exports = async (request, context) => {
    let event = request.body;
    console.log("CheckSound: Event:", event);

    // Augment event with location and sensorID
    event.location = event.originalEvent ? event.originalEvent.sensorID : 0;
    event.sensorID = event.originalEvent ? event.originalEvent.sensorID : 0;

    // Sync parallel calls to csl and csa
    let calls = [];
    calls.push(callFunction("csl", event, true, request.headers));
    calls.push(callFunction("csa", event, true, request.headers));

    console.log("CheckSound has called Loud and Accident");

    let results = await Promise.all(calls);

    return context.status(200).succeed({
        from: "CheckSound",
        calls: results,
    });
};
