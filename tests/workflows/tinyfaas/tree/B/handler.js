// Tree-B: Intermediate function
// Express-style handler (req, res)

import axios from "axios";

const GATEWAY_BASE = process.env.TINYFAAS_GATEWAY_URL || "http://tinyfaas.com";

function callFunction(functionName, data, sync, incomingHeaders) {
    return (async () => {
        const pathPrefix = sync ? "/fn/" : "/async-fn/";
        const url = new URL(`${GATEWAY_BASE}${pathPrefix}tree-${functionName}`);

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
            console.error(
                `Error calling ${functionName}:`,
                err && err.message ? err.message : err,
            );
            throw err;
        }
    })();
}

export default async (req, res) => {
    console.log("Event for B:", req.body);

    let checked = [];

    checked.push(await callFunction("d", { test: "event" }, true, req.headers));
    checked.push(await callFunction("e", { test: "event" }, true, req.headers));

    await new Promise((resolve) => setTimeout(resolve, 500));

    console.log("B: Checked are", checked);

    res.json({
        from: "B",
        input: req.body,
        checked: checked,
    });
};
