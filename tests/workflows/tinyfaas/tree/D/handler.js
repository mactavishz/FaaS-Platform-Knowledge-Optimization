// Tree-D: Leaf function
// Express-style handler (req, res)

export default async (req, res) => {
    console.log("Event for D:", req.body);

    await new Promise((resolve) => setTimeout(resolve, 500));

    res.json({
        from: "D",
    });
};