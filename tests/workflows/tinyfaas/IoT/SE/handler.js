// IoT-SE: StoreEvent - persists sensor data
// Express-style handler (req, res)
// DynamoDB calls replaced with simulated latency

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Simulated DynamoDB PutItem latency (configurable via env)
const DDB_PUT_MS = parseInt(process.env.IOT_DDB_PUT_MS) || 25;

module.exports = async (req, res) => {
    const event = req.body;
    console.log("StoreEvent: Event:", event);

    // Original: ddb.putItem to SensorDataTable with SensorID and Message
    // Simulating DynamoDB PutItem latency
    console.log(`Simulating DynamoDB PutItem (${DDB_PUT_MS}ms)...`);
    await sleep(DDB_PUT_MS);

    res.json({
        from: "StoreEvent",
        simulated: true,
        sensorID: event.sensorID,
    });
};
