// IoT-I: AnalyzeSensor - Entry function for tinyFaaS
// Express-style handler (req, res)

import got from "got";

const GATEWAY_BASE = process.env.TINYFAAS_GATEWAY_URL || "http://tinyfaas.com";

// Helper to call another function via the gateway
function callFunction(functionName, data, sync, incomingHeaders) {
    return (async () => {
        const url = new URL(`${GATEWAY_BASE}/fn/iot-${functionName}`);

        const headers = Object.assign({}, incomingHeaders || {});
        delete headers.host;
        delete headers["content-length"];
        headers["content-type"] = "application/json";

        if (!sync) {
            headers["x-tinyfaas-async"] = "true";
        } else {
            delete headers["x-tinyfaas-async"];
        }

        try {
            const res = await got.post(url, {
                json: data,
                headers,
                retry: { limit: 0 },
                throwHttpErrors: false,
                followRedirect: false,
                responseType: "text",
            });

            if (!sync) {
                return {};
            }

            try {
                return res.body ? JSON.parse(res.body) : {};
            } catch {
                return {};
            }
        } catch (err) {
            console.error(`Error calling ${functionName}:`, err && err.message ? err.message : err);
            throw err;
        }
    })();
}

function randn_bm() {
    var u = 0, v = 0;
    while (u === 0) u = Math.random(); //Converting [0,1) to (0,1)
    while (v === 0) v = Math.random(); //Converting [0,1) to (0,1)
    return Math.sqrt(-2.0 * Math.log(u)) * Math.cos(2.0 * Math.PI * v);
}

export default async (req, res) => {
    console.log("AnalyzeSensor: Event:", req.body);
    
    let sensorID = Math.floor(Math.random() * 101);
    let event = {
        Temperature: {
            sensorID: sensorID,
            value: (randn_bm() * 50) - 10, // Between -10 and 40
        },
        Sound: {
            sensorID: sensorID,
            value: (randn_bm() * 50) - 10, // Between -10 and 40
        },
        AirQuality: {
            sensorID: sensorID,
            value: (randn_bm() * 50) - 10, // Between -10 and 40
        },
        EmergencyVehicle: {
            sensorID: sensorID,
            value: (randn_bm() * 50) - 10, // Between -10 and 40
        },
    };
    
    console.log("Got Input Event", event);
    
    const headers = req.headers;
    // Sync call to cw (CheckSensor)
    const cwResult = await callFunction("cw", { originalEvent: event.Temperature }, true, headers);
    if (!cwResult.valid) {
        console.log("AnalyzeSensor: CheckSensor data invalid.");
    }

    // Sync call to se (StoreEvent)
    await callFunction("se", event.Temperature, true, headers);

    // Async fan-out to ct, cs, ca
    let calls = [];
    calls.push(callFunction("ct", { originalEvent: event.Temperature }, false, headers));
    calls.push(callFunction("cs", { originalEvent: event.Sound }, false, headers));
    calls.push(callFunction("ca", { originalEvent: event.AirQuality }, false, headers));
    
    console.log("AnalyzeSensor: Waiting for async calls:", calls.length);
    
    const results = await Promise.all(calls);
    
    console.log("AnalyzeSensor: All Promises done, results:", results);
    
    res.json({ results: results });
};
