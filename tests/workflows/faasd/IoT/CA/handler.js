'use strict';

// IoT-CA: CheckAir - calls DJ sync, then AS async
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
            console.error(`Error calling ${functionName}:`, err && err.message ? err.message : err);
            throw err;
        }
    })();
}

module.exports = async (request, context) => {
    const event = request.body;
    console.log("CheckAir: Event:", event);

    let sensorID = event.originalEvent ? event.originalEvent.sensorID : 0;
    let chain = 5;

    // Sync call to dj (DetectJam)
    let djResult = await callFunction("dj", event, true, request.headers);
    console.log("Got Response from DetectJam:", djResult);

    // Async call to as (ActionSignage)
    console.log("CheckAir is calling Signage async");
    let actionSignage = await callFunction("as", { location: sensorID, chain: chain }, false, request.headers);

    return context.status(200).succeed({
        from: "CheckAir",
        actionSignage: actionSignage,
    });
};
