'use strict';

// IoT-CSA: CheckSoundAccident - CPU work + Supabase write
// Express-style handler (req, res)
// DynamoDB simulation replaced with Supabase

const { createClient } = require("@supabase/supabase-js");

const SUPABASE_URL = process.env.SUPABASE_URL;
const SUPABASE_KEY = process.env.SUPABASE_KEY;

const USE_CASE_TABLE = "use_case";
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
    var primes = [];
    if (limit >= 2) {
        var sqrtlmt = Math.sqrt(limit) - 2;
        var nums = new Array();
        for (var i = 2; i <= limit; i++)
            nums.push(i);
        for (var i = 0; i <= sqrtlmt; i++) {
            var p = nums[i];
            if (p)
                for (var j = p * p - 2; j < nums.length; j += p)
                    nums[j] = 0;
        }
        for (var i = 0; i < nums.length; i++) {
            var p = nums[i];
            if (p)
                primes.push(p);
        }
    }
    return primes;
}

module.exports = async (request, context) => {
    const event = request.body;
    console.log("CheckSoundAccident: Event:", event);

    let times = 500_000;
    try {
        if (event.sieve) {
            times = parseInt(event.sieve);
        }
    } catch (err) {
        // Use default
    }

    // CPU-intensive work
    let start = Date.now();
    let primes = eratosthenes(times);
    let duration = Date.now() - start;
    console.log("Took time:", duration, "For length", primes.length);

    let dbRes;
    try {
         dbRes = await getSupabase().from(USE_CASE_TABLE).upsert(
            {
                sensor_id: 1001,
                message: event,
            },
            { onConflict: "sensor_id" },
        ).select();
    } catch (err) {
        await new Promise(resolve => setTimeout(resolve, 100)) // Sleep 100ms if this doesnt work
    }
    return context.status(200).succeed({
        from: "CheckSoundAccident",
        result: dbRes,
    });
};
