// IoT-CSA: CheckSoundAccident - CPU work + simulated DynamoDB write
// Express-style handler (req, res)
// DynamoDB calls replaced with simulated latency

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Simulated DynamoDB PutItem latency (configurable via env)
const DDB_PUT_MS = parseInt(process.env.IOT_DDB_PUT_MS) || 25;

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

export default async (req, res) => {
    const event = req.body;
    console.log("CheckSoundAccident: Event:", event);

    let times = 500000;
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

    // Original: ddb.putItem to UseCaseTable with SensorID=1001
    // Simulating DynamoDB PutItem latency
    // let params = {
    //     TableName: "UseCaseTable",
    //     Item : {
    //         'SensorID': {N: '1001'},
    //         'Message': {S: JSON.stringify(event)}
    //     }
    // }
    console.log(`Simulating DynamoDB PutItem (${DDB_PUT_MS}ms)...`);

    let result = null
    try {
        result = await sleep(DDB_PUT_MS);
    } catch (error) {
        await new Promise(resolve => setTimeout(resolve, 100)) // Sleep 100ms if this doesnt work
    }

    res.json({
        from: "CheckSoundAccident",
        result: result,
    });
};
