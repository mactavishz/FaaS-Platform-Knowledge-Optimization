'use strict';

// Webshop-shipmentquote: ShipmentQuote - calculates shipping cost based on total item quantity
// Express-style handler (req, res)
// Shipping rate: $1.50 per unit of quantity

module.exports = async (request, context) => {
    const event = request.body;
    console.log("ShipmentQuote: Event:", event);

    const cart = event.items || [];
    const totalQty = cart.reduce((acc, item) => acc + (item.quantity || 0), 0);
    const totalPrice = totalQty * 1.5;

    return context.status(200).succeed({
        costUsd: {
            currencyCode: "USD",
            units: totalPrice,
            nanos: 0,
        },
    });
};
