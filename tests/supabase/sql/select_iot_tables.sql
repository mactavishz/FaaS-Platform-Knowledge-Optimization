\pset pager off

select format('public.%I', :'sensor_data_table') as table_name;
select *
from public.:"sensor_data_table"
order by sensor_id;

select '' as separator;
select format('public.%I', :'use_case_table') as table_name;
select *
from public.:"use_case_table"
order by sensor_id;
