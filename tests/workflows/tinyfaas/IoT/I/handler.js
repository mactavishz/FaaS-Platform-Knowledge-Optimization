// IoT-I: AnalyzeSensor - Entry function for tinyFaaS
// Express-style handler (req, res)

const http = require("http");

const GATEWAY_BASE = "http://tinyfaas.com";

// Helper to call another function via the gateway
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
            delete headers["X-Tinyfaas-Async"];
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
                    // Async call returns immediately
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

function randn_bm() {
    var u = 0, v = 0;
    while (u === 0) u = Math.random();
    while (v === 0) v = Math.random();
    return Math.sqrt(-2.0 * Math.log(u)) * Math.cos(2.0 * Math.PI * v);
}

module.exports = async (req, res) => {
    console.log("AnalyzeSensor: Event:", req.body);
    
    let sensorID = Math.floor(Math.random() * 101);
    let event = {
        Temperature: {
            sensorID: sensorID,
            value: (randn_bm() * 50) - 10,
        },
        Sound: {
            sensorID: sensorID,
            value: (randn_bm() * 50) - 10,
        },
        AirQuality: {
            sensorID: sensorID,
            value: (randn_bm() * 50) - 10,
        },
        EmergencyVehicle: {
            sensorID: sensorID,
            value: (randn_bm() * 50) - 10,
        },
    };
    
    console.log("Got Input Event", event);
    
    const headers = req.headers;
    // Sync call to cw (CheckSensor)
    await callFunction("cw", { originalEvent: event.Temperature }, true, headers);

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
