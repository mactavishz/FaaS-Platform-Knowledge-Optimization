begin;

drop table if exists public.sensor_data cascade;
drop table if exists public.use_case cascade;
drop function if exists public.set_updated_at();

commit;
