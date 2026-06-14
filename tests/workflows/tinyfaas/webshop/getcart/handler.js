// Webshop-getcart: GetCart - fetches all cart items for a user via cartstorage
// Express-style handler (req, res)

import axios from "axios";

const GATEWAY_BASE = process.env.TINYFAAS_GATEWAY_URL || "http://tinyfaas.com";

function callFunction(functionName, data, sync, incomingHeaders) {
    return (async () => {
        const pathPrefix = sync ? "/fn/" : "/async-fn/";
        const url = new URL(`${GATEWAY_BASE}${pathPrefix}webshop-${functionName}`);

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

export default async (req, res) => {
    const event = req.body;
    console.log("GetCart: Event:", event);

    const headers = req.headers;
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

    res.json(cart);
};
