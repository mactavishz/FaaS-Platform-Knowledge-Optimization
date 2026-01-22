// IoT-CA: CheckAir - calls DJ sync, then AS async
// Express-style handler (req, res)

const http = require("http");

const GATEWAY_BASE = "http://tinyfaas.com";

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
    console.log("CheckAir: Event:", event);

    let sensorID = event.originalEvent ? event.originalEvent.sensorID : 0;
    let chain = 5;

    // Sync call to dj (DetectJam)
    let djResult = await callFunction("dj", event, true, req.headers);
    console.log("Got Response from DetectJam:", djResult);

    // Async call to as (ActionSignage)
    console.log("CheckAir is calling Signage async");
    let actionSignage = await callFunction("as", { location: sensorID, chain: chain }, false, req.headers);

    res.json({
        from: "CheckAir",
        actionSignage: actionSignage,
    });
};