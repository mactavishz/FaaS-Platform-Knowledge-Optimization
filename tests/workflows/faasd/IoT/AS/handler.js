'use strict';

// IoT-AS: ActionSignage - CPU work + Supabase writes
// Express-style handler (req, res)
// DynamoDB simulation replaced with Supabase

const { Worker } = require("node:worker_threads");

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

module.exports = async (request, context) => {
  const event = request.body;
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

  const sb = getSupabase();
  const promises = [];
  for (let currId = startLoc; currId <= endLoc; currId++) {
    try {
      promises.push(sb
        .from(USE_CASE_TABLE)
        .upsert({
            sensor_id: currId,
            message: event,
        }, { onConflict: "sensor_id" }).select());
    } catch (err) {
      console.log("Failed to queue upsert for sensor_id:", currId);
      console.log(err)
      await new Promise(resolve => setTimeout(resolve, 100)) // Sleep 100ms if this doesnt work
    }
  }

  await w1;
  await w2;
  const result = await Promise.all(promises);

  return context.status(200).succeed({
    from: "ActionSignage",
    useCase: result
  });
};
