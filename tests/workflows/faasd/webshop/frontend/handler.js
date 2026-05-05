'use strict';

// Webshop-frontend: entry point for the webshop
// Express-style handler (req, res)
//
// Supported operations (body.operation):
//   "get"       - homepage: currencies, products (currency-converted), ads, cart, recommendations
//   "cart"      - cart view with shipment quote (sync)
//   "addcart"   - add a product to the cart, return updated cart (sync)
//   "checkout"  - delegate to the checkout orchestrator (sync)
//   "emptycart" - clear the user's cart (async)

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
    console.log("Frontend: Event:", event);

    const headers = request.headers;
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

            return context.status(200).succeed({
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
            return context.status(200).succeed({ cart, shippingCost });
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
                true,
                headers,
            );
            const updatedCart = await callFunction("getcart", { userId }, true, headers);
            return context.status(200).succeed({ cart: updatedCart });
            break;
        }

        case "checkout": {
            // Delegate to the checkout orchestrator (synchronous)
            await callFunction(
                "checkout",
                {
                    userId,
                    currency: currencyPref,
                    address: event.address,
                    email: event.email,
                    creditCard: event.creditCard || { creditCardNumber: event.creditCardNumber },
                },
                true,
                headers,
            );
            return context.status(200).succeed({ userId });
            break;
        }

        case "emptycart": {
            // Empty the user's cart (fire-and-forget)
            await callFunction("emptycart", { userId }, false, headers);
            return context.status(200).succeed({ userId });
            break;
        }

        default: {
            return context.status(200).succeed({ error: `Unknown operation: ${operation}` });
        }
    }
};
