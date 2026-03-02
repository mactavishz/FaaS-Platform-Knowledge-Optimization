// Tree-B: Intermediate function
// Express-style handler (req, res)

import got from "got";

const GATEWAY_BASE = "http://tinyfaas.com";

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
