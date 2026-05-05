'use strict';

// Webshop-getcart: GetCart - fetches all cart items for a user via cartstorage
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
    console.log("GetCart: Event:", event);

    const headers = request.headers;
    const userId = event.userId || "0";

    // Fetch all items for this user from cartstorage
    const items = await callFunction("cartstorage", { operation: "get", userId }, true, headers);

    // Normalise the response: cartstorage returns plain Supabase rows
    // { user_id, item_id, quantity, ... }
    const cart = (Array.isArray(items) ? items : []).map((row) => ({
        itemId: row.item_id,
        userId: row.user_id,
        quantity: row.quantity,
    }));

    return context.status(200).succeed(cart);
};
