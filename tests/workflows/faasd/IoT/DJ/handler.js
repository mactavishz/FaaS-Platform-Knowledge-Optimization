'use strict';

// IoT-DJ: DetectJam - CPU work + Supabase write
// Express-style handler (req, res)
// DynamoDB simulation replaced with Supabase

const { Worker } = require("node:worker_threads");

const { createClient } = require("@supabase/supabase-js");

const SUPABASE_URL = process.env.SUPABASE_URL;
const SUPABASE_KEY = process.env.SUPABASE_KEY;

const USE_CASE_TABLE = "faasd_use_case";
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

const js_string = `
const { workerData, parentPort } = require('worker_threads');
let num = workerData.num || 7;
let res = cpu_intensive(num);
parentPort.postMessage(res);

function cpu_intensive(baseNumber) {
    let result = 0;
    for (var i = Math.pow(baseNumber, 7); i >= 0; i--) {
        result += Math.atan(i) * Math.tan(i);
    }
    return result;
}
`;

module.exports = async (request, context) => {
    const event = request.body;
    console.log("DetectJam: Event:", event);

    // CPU-intensive work via worker threads
    let w1 = new Promise((resolve, reject) => {
        const worker = new Worker(js_string, { workerData: {}, eval: true });
        worker.on("message", (m) => resolve(m));
        worker.on("error", (m) => reject(m));
    });
    let w2 = new Promise((resolve, reject) => {
        const worker = new Worker(js_string, { workerData: {}, eval: true });
        worker.on("message", (m) => resolve(m));
        worker.on("error", (m) => reject(m));
    });

    const dbRes = await getSupabase().from(USE_CASE_TABLE).upsert(
        {
            sensor_id: 998,
            message: event,
        },
        { onConflict: "sensor_id" },
    ).select();
    await w1;
    await w2;

    return context.status(200).succeed({
        from: "DetectJam",
        useCase: dbRes
    });
};
