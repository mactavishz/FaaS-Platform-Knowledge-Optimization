// IoT-CSL: CheckSoundLoud - simulated DynamoDB queries + conditional write
// Express-style handler (req, res)
// DynamoDB calls replaced with simulated latency

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Simulated DynamoDB latencies (configurable via env)
const DDB_GET_MS = parseInt(process.env.IOT_DDB_GET_MS) || 20;
const DDB_PUT_MS = parseInt(process.env.IOT_DDB_PUT_MS) || 25;

export default async (req, res) => {
    const event = req.body;
    console.log("CheckSoundLoud: Event:", event);

    let callingEvent = event.originalEvent || {};
    let sensorID = callingEvent.sensorID || 0;

    // Original: two DynamoDB queries for neighbor sensor IDs (sensorID+1 and sensorID-1)
    console.log(`Simulating DynamoDB Query for sensorID ${sensorID + 1} (${DDB_GET_MS}ms)...`);
    await sleep(DDB_GET_MS);

    console.log(`Simulating DynamoDB Query for sensorID ${sensorID - 1} (${DDB_GET_MS}ms)...`);
    await sleep(DDB_GET_MS);

    console.log("Doing some magic with nextTemp and beforeTemp");
    let isTooLoud = true; // Original: hardcoded to true

    console.log("IsTooLoud:", isTooLoud);

    if (isTooLoud) {
        // Original: ddb.putItem to UseCaseTable with SensorID=1500
        console.log(`Simulating DynamoDB PutItem for alert (${DDB_PUT_MS}ms)...`);
        await sleep(DDB_PUT_MS);

        res.json({
            from: "CheckSoundLoud",
            simulated: true,
            isTooLoud: true,
        });
    } else {
        res.json({});
    }
};
