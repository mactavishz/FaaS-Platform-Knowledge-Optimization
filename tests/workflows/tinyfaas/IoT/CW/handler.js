// IoT-CW: CheckSensor - CPU-intensive validation
// Express-style handler (req, res)

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

export default (req, res) => {
    const event = req.body;
    console.log("CheckSensor: Event:", event);

    let times = 500000;
    try {
        if (event.sieve) {
            times = parseInt(event.sieve);
        }
    } catch (err) {
        // Use default
    }

    let start = Date.now();
    let primes = eratosthenes(times);

    res.json({
        valid: true,
        eratosthenes: primes,
        time: Date.now() - start,
    });
};
