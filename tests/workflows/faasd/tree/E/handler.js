'use strict';

// Tree-E: Leaf function
// Express-style handler (req, res)

module.exports = async (request, context) => {
    console.log("Event for E:", request.body);

    await new Promise((resolve) => setTimeout(resolve, 500));

    return context.status(200).succeed({
        from: "E",
    });
};