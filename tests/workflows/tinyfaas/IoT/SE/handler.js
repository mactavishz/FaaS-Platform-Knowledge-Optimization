// IoT-SE: StoreEvent - persists sensor data
// Express-style handler (req, res)
// DynamoDB calls replaced with simulated latency

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Simulated DynamoDB PutItem latency (configurable via env)
const DDB_PUT_MS = parseInt(process.env.IOT_DDB_PUT_MS) || 25;

export default async (req, res) => {
    const event = req.body;
    console.log("StoreEvent: Event:", event);

    // Original: ddb.putItem to SensorDataTable with SensorID and Message
    // Simulating DynamoDB PutItem latency
    // let params = {
    //     TableName: "SensorDataTable",
    //     Item : {
    //         'SensorID': {N: event["sensorID"] + ''},
    //         'Message': {S: JSON.stringify(event)}
    //     }
    // }
    console.log(`Simulating DynamoDB PutItem (${DDB_PUT_MS}ms)...`);
    await sleep(DDB_PUT_MS);

    res.json({
        from: "StoreEvent",
        simulated: true,
        sensorID: event.sensorID,
    });
};
