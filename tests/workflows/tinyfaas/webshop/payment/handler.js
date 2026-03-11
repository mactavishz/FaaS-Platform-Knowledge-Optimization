// Webshop-payment: Payment - simulates payment processing with CPU load (Sieve of Eratosthenes)
// Express-style handler (req, res)
// Note: this function is part of the webshop reference architecture but is not called
// by the current workflow graph. It is included for completeness.

import { randomBytes } from "crypto";

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

export default async (req, res) => {
    const event = req.body;
    console.log("Payment: Event:", event);

    // CPU simulation
    eratosthenes(500_000);

    res.json({ transactionId: randomBytes(16).toString("hex") });
};
