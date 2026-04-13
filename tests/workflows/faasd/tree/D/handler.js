'use strict';

// Tree-D: Leaf function
// Express-style handler (req, res)

module.exports = async (request, context) => {
    console.log("Event for D:", request.body);

    await new Promise((resolve) => setTimeout(resolve, 500));

    return context.status(200).succeed({
        from: "D",
    });
};