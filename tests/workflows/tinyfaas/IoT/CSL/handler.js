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

    // let params = {
    //     TableName: "UseCaseTable",
    //     KeyConditionExpression: "#sd = :sid",
    //     ExpressionAttributeNames:{
    //         "#sd": "SensorID"
    //     },
    //     ExpressionAttributeValues: {
    //         ":sid": {N: '' + (callingEvent["sensorID"] + 1)}
    //     }
    // }
    // console.log("Querying with Params (1)", params)
    // Original: two DynamoDB queries for neighbor sensor IDs (sensorID+1 and sensorID-1)
    let nextTemp = null
    try {
        console.log(`Simulating DynamoDB Query for sensorID ${sensorID + 1} (${DDB_GET_MS}ms)...`);
        nextTemp = await sleep(DDB_GET_MS);
    } catch (error) {
        await new Promise(resolve => setTimeout(resolve, 100)) // Sleep 100ms if this doesnt work
    }

    // params = {
    //     TableName: "UseCaseTable",
    //     KeyConditionExpression: "#sd = :sid",
    //     ExpressionAttributeNames:{
    //         "#sd": "SensorID"
    //     },
    //     ExpressionAttributeValues: {
    //         ":sid": {N: (callingEvent["sensorID"] - 1) + ''}
    //     }
    // }
    // console.log("Querying with Params (2)", params)

    console.log(`Simulating DynamoDB Query for sensorID ${sensorID - 1} (${DDB_GET_MS}ms)...`);
    await sleep(DDB_GET_MS);

    console.log("Doing some magic with nextTemp and beforeTemp");
    let isTooLoud = true; // Original: hardcoded to true

    console.log("IsTooLoud:", isTooLoud);

    if (isTooLoud) {
        // Original: ddb.putItem to UseCaseTable with SensorID=1500
        // Set an Alert so that something can happen. I dont know, maybe a technichan would look at the site or whatever
        // params = {
        //     TableName: "UseCaseTable",
        //     Item : {
        //         'SensorID': {N: '1500'},
        //         'Message': {S: JSON.stringify(event)}
        //     }
        // }
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
