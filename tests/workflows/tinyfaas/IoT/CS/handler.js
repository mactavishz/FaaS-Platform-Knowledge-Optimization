// IoT-CS: CheckSound - calls CSL and CSA in parallel (sync)
// Express-style handler (req, res)

import got from "got";

const GATEWAY_BASE = "http://tinyfaas.com";

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
