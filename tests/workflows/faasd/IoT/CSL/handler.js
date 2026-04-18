'use strict';

// IoT-CSL: CheckSoundLoud - Supabase queries + conditional write
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

module.exports = async (request, context) => {
    const event = request.body;
    console.log("CheckSoundLoud: Event:", event);

    let callingEvent = event.originalEvent || {};
    let sensorID = callingEvent.sensorID || 0;

    let nextRes, prevRes;
    const sb = getSupabase();
    try {
        nextRes = await sb.from(USE_CASE_TABLE)
            .select("sensor_id,message,updated_at")
            .eq("sensor_id", sensorID + 1)
            .maybeSingle();
    } catch (err) {
        console.log("CheckSoundLoud: failed to query next", err);
        await new Promise(resolve => setTimeout(resolve, 100)) // Sleep 100ms if this doesnt work
    }
    
    try {
        prevRes = await sb.from(USE_CASE_TABLE)
            .select("sensor_id,message,updated_at")
            .eq("sensor_id", sensorID - 1)
            .maybeSingle();
    } catch (err) {
        console.log("CheckSoundLoud: failed to query prev", err);
        await new Promise(resolve => setTimeout(resolve, 100)) // Sleep 100ms if this doesnt work
    }

    console.log("Doing some magic with nextTemp and beforeTemp: ", nextRes, prevRes);
    let isTooLoud = true; // preserved behavior

    console.log("IsTooLoud:", isTooLoud);

    if (isTooLoud) {
        const dbRes = await sb.from(USE_CASE_TABLE).upsert(
            {
                sensor_id: 1500,
                message: event,
            },
            { onConflict: "sensor_id" },
        );

        return context.status(200).succeed({
            from: "CheckSoundLoud",
            useCase: dbRes
        });
    }

    return context.status(200).succeed({});
};
