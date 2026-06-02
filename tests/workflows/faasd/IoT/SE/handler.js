'use strict';

// IoT-SE: StoreEvent - persists sensor data
// Express-style handler (req, res)
// DynamoDB simulation replaced with Supabase

const { createClient } = require("@supabase/supabase-js");

const SUPABASE_URL = process.env.SUPABASE_URL;
const SUPABASE_KEY = process.env.SUPABASE_KEY;

const SENSOR_DATA_TABLE = "faasd_sensor_data";
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

module.exports = async (request, context) => {
  const event = request.body;
  console.log("StoreEvent: Event:", event);

  const sensorId = Number.parseInt(String(event?.sensorID ?? "0"), 10);
  const dbRes = await getSupabase()
    .from(SENSOR_DATA_TABLE)
    .upsert(
      {
        sensor_id: Number.isFinite(sensorId) ? sensorId : 0,
        message: event,
      },
      { onConflict: "sensor_id" },
    ).select();
  console.log("StoreEvent: DB Result:", dbRes);
  return context.status(200).succeed(dbRes);
};
