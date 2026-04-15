// Tree-A: Entry function
// Express-style handler (req, res)

import axios from "axios";

const GATEWAY_BASE = process.env.TINYFAAS_GATEWAY_URL || "http://tinyfaas.com";

function callFunction(functionName, data, sync, incomingHeaders) {
    return (async () => {
        const url = new URL(`${GATEWAY_BASE}/fn/tree-${functionName}`);

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
                timeout: 30000,
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
    console.log("Event for A:", req.body);

    let calls = [];
    let checked = [];

    calls.push(callFunction("c", { test: "event" }, false, req.headers));
    checked.push(await callFunction("b", { test: "event" }, true, req.headers));

    let results = await Promise.all(calls);

    console.log("Results are", results);
    console.log("Checked are", checked);

    res.json({
        from: "A",
        traceId: req.body?.traceId,
        results: results,
        checked: checked,
    });
};
