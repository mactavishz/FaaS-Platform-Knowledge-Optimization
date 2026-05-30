import { Trend } from 'k6/metrics';
import {
  checkoutAddress,
  checkoutCreditCard,
  checkoutEmail,
  createBaseTags,
  createOptions,
  createUserId,
  currency,
  invokeFrontend,
  randomCartItem,
  validateAddcartPayload,
  validateCheckoutPayload,
  waitForInitialScaleDown,
} from './lib/webshop.js';

const addcartMs = new Trend('addcart_latency_ms');
const checkoutMs = new Trend('checkout_latency_ms');

export const options = createOptions();

export default function () {
  const userId = createUserId();
  const baseTags = createBaseTags();

  waitForInitialScaleDown({ ...baseTags, phase: 'before_journey' });

  const product = randomCartItem();
  const productTags = {
    ...baseTags,
    product_id: product.productId,
    quantity: String(product.quantity),
  };
  const addcartResult = invokeFrontend(
    'addcart',
    {
      userId,
      productId: product.productId,
      quantity: product.quantity,
    },
    { ...productTags, step: 'addcart' },
  );
  addcartMs.add(addcartResult.response.timings.duration, {
    ...productTags,
    step: 'addcart',
  });
  validateAddcartPayload(addcartResult.body, product, baseTags);

  const checkoutResult = invokeFrontend(
    'checkout',
    {
      userId,
      currency,
      address: checkoutAddress,
      email: checkoutEmail,
      creditCard: checkoutCreditCard,
    },
    { ...baseTags, step: 'checkout' },
  );
  checkoutMs.add(checkoutResult.response.timings.duration, { ...baseTags, step: 'checkout' });
  validateCheckoutPayload(checkoutResult.body, userId, baseTags);
}
