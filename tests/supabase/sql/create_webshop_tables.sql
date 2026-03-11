begin;

-- Create a reusable function for updating the 'updated_at' column (shared with IoT tables)
create or replace function public.set_updated_at()
returns trigger as $$
begin
  new.updated_at = now();
  return new;
end;
$$ language plpgsql;

-- Webshop cart table: stores shopping cart items per user
create table if not exists public.webshop_cart (
  user_id    text        not null,
  item_id    text        not null,
  quantity   integer     not null default 1,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (user_id, item_id)
);

alter table public.webshop_cart
enable row level security;

create index if not exists webshop_cart_user_id_idx
  on public.webshop_cart (user_id);

create index if not exists webshop_cart_updated_at_idx
  on public.webshop_cart (updated_at);

-- Auto-update 'updated_at' on every row modification
create or replace trigger trg_webshop_cart_updated_at
  before update on public.webshop_cart
  for each row
  execute function public.set_updated_at();

commit;
