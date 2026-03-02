// Tree-E: Leaf function
// Express-style handler (req, res)

export default async (req, res) => {
    console.log("Event for E:", req.body);

    await new Promise((resolve) => setTimeout(resolve, 500));

    res.json({
        from: "E",
    });
};