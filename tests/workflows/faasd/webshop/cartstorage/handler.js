'use strict';

// Webshop-cartstorage: CartStorage - Supabase-backed cart key-value store
// Express-style handler (req, res)
// Replaces original DynamoDB-backed cartkvstorage with Supabase/PostgreSQL.
//
// Supported operations:
//   get   - retrieve all cart items for a user
//   add   - upsert a cart item (userId + itemId composite key)
//   empty - delete all cart items for a user

const { createClient } = require("@supabase/supabase-js");

const SUPABASE_URL = process.env.SUPABASE_URL;
const SUPABASE_KEY = process.env.SUPABASE_KEY;

const CART_TABLE = "webshop_cart";
const SUPABASE_SCHEMA = "public";

let supabase;
function getSupabase() {
    if (supabase) return supabase;

    if (!SUPABASE_URL || !SUPABASE_KEY) {
        const missing = [
            !SUPABASE_URL ? "SUPABASE_URL" : null,
            !SUPABASE_KEY ? "SUPABASE_KEY" : null,
        ].filter(Boolean);
        throw new Error(`Missing required env vars: ${missing.join(", ")}`);
    }

    supabase = createClient(SUPABASE_URL, SUPABASE_KEY, {
        auth: {
            persistSession: false,
            autoRefreshToken: false,
            detectSessionInUrl: false,
        },
        db: { schema: SUPABASE_SCHEMA },
    });
    return supabase;
}

function eratosthenes(limit) {
    const primes = [];
    if (limit >= 2) {
        const sqrtlmt = Math.sqrt(limit) - 2;
        const nums = [];
        for (let i = 2; i <= limit; i++) nums.push(i);
        for (let i = 0; i <= sqrtlmt; i++) {
            const p = nums[i];
            if (p) {
                for (let j = p * p - 2; j < nums.length; j += p) nums[j] = 0;
            }
        }
        for (let i = 0; i < nums.length; i++) {
            if (nums[i]) primes.push(nums[i]);
        }
    }
    return primes;
}

module.exports = async (request, context) => {
    const event = request.body;
    console.log("CartStorage: Event:", event);

    const operation = String(event.operation || "").toLowerCase();
    const userId = String(event.userId || "");

    if (operation === "get") {
        // Return all cart items for the given user
        const dbRes = await getSupabase()
            .from(CART_TABLE)
            .select("*")
            .eq("user_id", userId);

        console.log("CartStorage: get result:", dbRes);
        return context.status(200).succeed(dbRes.data || []);

    } else if (operation === "add") {
        const itemId = String(event.productId || event.item?.productId || "");
        const quantity = parseInt(event.quantity ?? event.item?.quantity ?? 1, 10);

        // Upsert the cart item; on conflict update quantity
        const dbRes = await getSupabase()
            .from(CART_TABLE)
            .upsert(
                { user_id: userId, item_id: itemId, quantity },
                { onConflict: "user_id,item_id" },
            )
            .select();

        // CPU simulation
        eratosthenes(500_000);

        console.log("CartStorage: add result:", dbRes);
        return context.status(200).succeed(dbRes.data || []);

    } else if (operation === "empty") {
        // Delete all items for this user in a single query
        const dbRes = await getSupabase()
            .from(CART_TABLE)
            .delete()
            .eq("user_id", userId);

        console.log("CartStorage: empty result:", dbRes);
        return context.status(200).succeed({});

    } else {
        console.error("CartStorage: unknown operation:", operation);
        return context.status(200).succeed({ error: `Unknown operation: ${operation}` });
    }
};
