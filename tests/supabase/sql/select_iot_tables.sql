\pset pager off

\echo 'public.sensor_data'
select *
from public.sensor_data
order by sensor_id;

\echo ''
\echo 'public.use_case'
select *
from public.use_case
order by sensor_id;
