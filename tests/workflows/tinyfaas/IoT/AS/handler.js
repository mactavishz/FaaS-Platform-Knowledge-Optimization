// IoT-AS: ActionSignage - CPU work + Supabase writes
// Express-style handler (req, res)
// DynamoDB simulation replaced with Supabase

import { Worker } from "node:worker_threads";

import { createClient } from "@supabase/supabase-js";

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

const js_string = `
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

export default async (req, res) => {
  const event = req.body;
  console.log("ActionSignage: Event:", event);

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

  // Calculate range for upserts
  let val1 = parseInt(event.location) || 0;
  let val2 = (parseInt(event.location) || 0) + (parseInt(event.chain) || 1);
  let startLoc = Math.min(val1, val2);
  let endLoc = Math.max(val1, val2);
  let itemCount = endLoc - startLoc + 1;

  try {
    const sb = getSupabase();
    const rows = [];
    for (let currId = startLoc; currId <= endLoc; currId++) {
      rows.push({ sensor_id: currId, message: event });
    }

    const dbWrite = sb
      .from(USE_CASE_TABLE)
      .upsert(rows, { onConflict: "sensor_id" });
    const [, , dbRes] = await Promise.all([w1, w2, dbWrite]);
    if (dbRes?.error) {
      throw new Error(`Supabase upsert failed: ${dbRes.error.message}`);
    }

    res.json({
      from: "ActionSignage",
      simulated: false,
      itemCount,
    });
  } catch (err) {
    console.error("ActionSignage: DB operation failed", err);
    res.status(500).json({
      error: "ActionSignage failed",
      message: err instanceof Error ? err.message : String(err),
    });
  }
};
