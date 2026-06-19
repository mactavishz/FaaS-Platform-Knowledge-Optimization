// IoT-CA: CheckAir - calls DJ sync, then AS async
// Express-style handler (req, res)

import axios from "axios";

const GATEWAY_BASE = process.env.TINYFAAS_GATEWAY_URL || "http://tinyfaas.com";

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
