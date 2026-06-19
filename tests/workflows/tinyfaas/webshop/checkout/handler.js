// Webshop-checkout: Checkout - orchestrates the full checkout flow
// Express-style handler (req, res)
//
// Flow:
//   1. [sync]  GetCart  -> CartStorage (select)
//   2. [sync]  ListProducts
//   3. [sync]  Currency (once per cart item + once for shipment cost)
//   4. [sync]  ShipmentQuote
//   5. [async] ShipOrder (CPU sim, fire-and-forget)
//   6. [async] Email    (CPU sim, fire-and-forget)
//   7. [async] EmptyCart -> CartStorage (delete, fire-and-forget)
//   Two worker threads run concurrently throughout to simulate backend processing.

import axios from "axios";
import { Worker } from "worker_threads";

const GATEWAY_BASE = process.env.TINYFAAS_GATEWAY_URL || "http://tinyfaas.com";

// Worker thread code (CommonJS, evaluated inline)
const WORKER_CODE = `
const { workerData, parentPort } = require('worker_threads');

let num = workerData.num || 7;
let res = cpu_intensive(num);
parentPort.postMessage(res);

// https://gist.github.com/sqren/5083d73f184acae0c5b7
function cpu_intensive(baseNumber) {
    let result = 0;
    for (var i = Math.pow(baseNumber, 7); i >= 0; i--) {
        result += Math.atan(i) * Math.tan(i);
    }
    return result;
}
`;

function spawnWorker() {
    return new Promise((resolve, reject) => {
        const worker = new Worker(WORKER_CODE, { workerData: {}, eval: true });
        worker.on("message", (m) => resolve(m));
        worker.on("error", (m) => reject(m));
    });
}

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
    console.log("Checkout: Event:", event);

    const headers = req.headers;
    const userId = String(event.userId || "0");
    const currencyPref = String(event.currency || "USD");

    // Fetch cart contents
    const cart = await callFunction("getcart", { userId }, true, headers);

    // Start two CPU worker threads concurrently to simulate backend processing
    const w1 = spawnWorker();
    const w2 = spawnWorker();

    // Fetch full product list
    const productsList = await callFunction("listproducts", {}, true, headers);

    // For each cart item, find its product and convert the price to the preferred currency
    const orderProducts = await Promise.all(
        cart.map(async (item) => {
            const product = (productsList.products || []).find((p) => p.id == item.itemId);
            if (!product) return item;
            const newPrice = await callFunction(
                "currency",
                { from: product.priceUsd, toCode: currencyPref },
                true,
                headers,
            );
            return Object.assign({}, product, { price: newPrice });
        }),
    );

    console.log("Checkout: OrderProducts:", orderProducts);

    // Get shipment quote and convert to preferred currency
    const shipmentPrice = await callFunction(
        "shipmentquote",
        { userId, items: cart },
        true,
        headers,
    );
    const convertedShipmentPrice = await callFunction(
        "currency",
        { from: shipmentPrice.costUsd, toCode: currencyPref },
        true,
        headers,
    );

    // Fire-and-forget side effects
    await callFunction("shiporder", { address: event.address, items: orderProducts }, false, headers);
    await callFunction("email", { message: "Your order has been shipped" }, false, headers);
    await callFunction("emptycart", { userId }, false, headers);

    // Wait for the CPU workers to complete
    await w1;
    await w2;

    res.json([orderProducts, convertedShipmentPrice]);
};
