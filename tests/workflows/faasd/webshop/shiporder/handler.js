'use strict';

// Webshop-shiporder: ShipOrder - simulates order fulfilment with CPU load (Sieve of Eratosthenes)
// Express-style handler (req, res)

const { randomBytes } = require("crypto");

function eratosthenes(limit) {
    const primes = [];
    if (limit >= 2) {
        const sqrtlmt = Math.sqrt(limit) - 2;
        const nums = [];
        for (let i = 2; i <= limit; i++) nums.push(i);
        for (let i = 0; i <= sqrtlmt; i++) {
            const p = nums[i];
            if (p) {
                for (let j = p * p - 2; j < nums.length; j += p) nums[j] = 0;
            }
        }
        for (let i = 0; i < nums.length; i++) {
            if (nums[i]) primes.push(nums[i]);
        }
    }
    return primes;
}

module.exports = async (request, context) => {
    const event = request.body;
    console.log("ShipOrder: Event:", event);

    // CPU simulation
    eratosthenes(500_000);

    return context.status(200).succeed({ id: randomBytes(16).toString("hex") });
};
