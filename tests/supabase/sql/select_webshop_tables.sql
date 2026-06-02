\pset pager off

select format('public.%I', :'cart_table') as table_name;
select *
from public.:"cart_table"
order by user_id, item_id;
