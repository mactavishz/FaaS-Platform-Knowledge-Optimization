// Webshop-listrecommendations: ListRecommendations - returns up to 2 products sharing
// categories with the given product IDs, excluding those products themselves
// Express-style handler (req, res)

import axios from "axios";

const GATEWAY_BASE = process.env.TINYFAAS_GATEWAY_URL || "http://tinyfaas.com";

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
            const res = await axios.post(url.toString(), data, {
                headers,
                timeout: 30000,
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
    console.log("ListRecommendations: Event:", event);

    const headers = req.headers;
    const productIds = Array.isArray(event.productIds) ? event.productIds : [];

    const productsList = await callFunction("listproducts", {}, true, headers);
    const allProducts = productsList.products || [];

    // Collect all categories of the requested products
    const inputProducts = allProducts.filter((p) => productIds.includes(p.id));
    const inputProductIds = inputProducts.map((p) => p.id);
    const categories = inputProducts.flatMap((p) => p.categories);

    // Find other products sharing at least one category
    const recommendations = allProducts
        .filter((p) => {
            if (inputProductIds.includes(p.id)) return false;
            return p.categories.some((cat) => categories.includes(cat));
        })
        .sort(() => 0.5 - Math.random())
        .slice(0, 2);

    res.json(recommendations);
};
