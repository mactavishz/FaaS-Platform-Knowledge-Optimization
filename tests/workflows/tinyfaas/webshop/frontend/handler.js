// Webshop-frontend: entry point for the webshop
// Express-style handler (req, res)
//
// Supported operations (req.body.operation):
//   "get"       - homepage: currencies, products (currency-converted), ads, cart, recommendations
//   "cart"      - cart view with shipment quote (sync)
//   "addcart"   - add a product to the cart, return updated cart (async)
//   "checkout"  - delegate to the checkout orchestrator (async)
//   "emptycart" - clear the user's cart (async)

import got from "got";

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
    console.log("Frontend: Event:", event);

    const headers = req.headers;
    const operation = String(event.operation || "get");
    const userId = String(event.userId || "0");
    const currencyPref = String(event.currency || "USD");

    switch (operation) {
        case "get": {
            // Full homepage: currencies + products (price-converted) + ads + cart + recommendations
            const supportedCurrencies = await callFunction("supportedcurrencies", {}, true, headers);
            const productsList = await callFunction("listproducts", {}, true, headers);

            const productsWithPrice = await Promise.all(
                (productsList.products || []).map(async (product) => {
                    const newPrice = await callFunction(
                        "currency",
                        { from: product.priceUsd, toCode: currencyPref },
                        true,
                        headers,
                    );
                    return Object.assign({}, product, { price: newPrice });
                }),
            );

            const ads = await callFunction("getads", {}, true, headers);
            const cart = await callFunction("getcart", { userId }, true, headers);
            const recommendations = await callFunction(
                "listrecommendations",
                { productIds: cart.map((item) => item.id) },
                true,
                headers,
            );

            res.json({
                supportedCurrencies,
                productsList: productsWithPrice,
                ads,
                cart,
                recommendations,
            });
            break;
        }

        case "cart": {
            // Cart view with shipping cost
            const cart = await callFunction("getcart", { userId }, true, headers);
            const shippingCost = await callFunction(
                "shipmentquote",
                { userId, items: cart },
                true,
                headers,
            );
            res.json({ cart, shippingCost });
            break;
        }

        case "addcart": {
            // Add item to cart, then return the updated cart
            await callFunction(
                "addcartitem",
                {
                    userId,
                    productId: event.productId || "0",
                    quantity: event.quantity || 1,
                },
                false,
                headers,
            );
            const updatedCart = await callFunction("getcart", { userId }, true, headers);
            res.json({ cart: updatedCart });
            break;
        }

        case "checkout": {
            // Delegate to the checkout orchestrator (fire-and-forget)
            await callFunction(
                "checkout",
                {
                    userId,
                    currency: currencyPref,
                    address: event.address,
                    email: event.email,
                    creditCard: event.creditCard || { creditCardNumber: event.creditCardNumber },
                },
                false,
                headers,
            );
            res.json({ userId });
            break;
        }

        case "emptycart": {
            // Empty the user's cart (fire-and-forget)
            await callFunction("emptycart", { userId }, false, headers);
            res.json({ userId });
            break;
        }

        default: {
            res.json({ error: `Unknown operation: ${operation}` });
        }
    }
};
