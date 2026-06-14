// IoT-CS: CheckSound - calls CSL and CSA in parallel (sync)
// Express-style handler (req, res)

import axios from "axios";

const GATEWAY_BASE = process.env.TINYFAAS_GATEWAY_URL || "http://tinyfaas.com";

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
    let event = req.body;
    console.log("CheckSound: Event:", event);

    // Augment event with location and sensorID
    event.location = event.originalEvent ? event.originalEvent.sensorID : 0;
    event.sensorID = event.originalEvent ? event.originalEvent.sensorID : 0;

    // Sync parallel calls to csl and csa
    let calls = [];
    calls.push(callFunction("csl", event, true, req.headers));
    calls.push(callFunction("csa", event, true, req.headers));

    console.log("CheckSound has called Loud and Accident");

    let results = await Promise.all(calls);

    res.json({
        from: "CheckSound",
        calls: results,
    });
};
