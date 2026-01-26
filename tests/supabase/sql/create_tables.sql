begin;

create table if not exists public.sensor_data (
  sensor_id bigint primary key,
  message jsonb not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists sensor_data_updated_at_idx
  on public.sensor_data (updated_at);

create table if not exists public.use_case (
  sensor_id bigint primary key,
  message jsonb not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists use_case_updated_at_idx
  on public.use_case (updated_at);

commit;
