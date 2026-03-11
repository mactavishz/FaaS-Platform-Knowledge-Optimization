// Webshop-getcart: GetCart - fetches all cart items for a user via cartstorage
// Express-style handler (req, res)

import got from "got";

const GATEWAY_BASE = "http://tinyfaas.com";

function callFunction(functionName, data, sync, incomingHeaders) {
    return (async () => {
        const url = new URL(`${GATEWAY_BASE}/fn/webshop-${functionName}`);

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

            if (!sync) return {};
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
