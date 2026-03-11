// Webshop-addcartitem: AddCartItem - adds a product to a user's cart via cartstorage
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
    console.log("AddCartItem: Event:", event);

    const headers = req.headers;

    // Async fire-and-forget: cartstorage handles the actual upsert
    await callFunction(
        "cartstorage",
        {
            operation: "add",
            userId: event.userId,
            productId: event.productId,
            quantity: event.quantity,
        },
        false,
        headers,
    );

    res.json(true);
};
