'use strict';

// Webshop-searchproducts: SearchProducts - searches products by name or description
// Express-style handler (req, res)

const axios = require("axios");

const GATEWAY_BASE = (process.env.FAASD_GATEWAY_URL || "http://127.0.0.1:8080").replace(/\/$/, "");

function callFunction(functionName, data, sync, incomingHeaders) {
    return (async () => {
        const suffix = sync ? "function" : "async-function";
        const url = new URL(`${GATEWAY_BASE}/${suffix}/webshop-${functionName}`);

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

            if (!sync) return {};
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
    console.log("SearchProducts: Event:", event);

    const headers = request.headers;
    const query = String(event.query || "");

    const productsList = await callFunction("listproducts", {}, true, headers);
    const results = (productsList.products || []).filter(
        (p) => p.name.includes(query) || p.description.includes(query),
    );

    return context.status(200).succeed(results);
};
