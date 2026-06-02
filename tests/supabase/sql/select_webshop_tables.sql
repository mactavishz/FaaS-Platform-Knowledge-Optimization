\pset pager off

\echo 'public.webshop_cart'
select *
from public.webshop_cart
order by user_id, item_id;
