-- The SQLite database databases.tape records: a coffee shop's customers,
-- products and orders, every name and address made up.
create table customers(
	id integer primary key,
	name text not null,
	email text,
	city text
);

insert into customers(name, email, city) values
	('Ada', 'ada@example.com', 'London'),
	('Grace', 'grace@example.com', 'Arlington'),
	('Linus', null, 'Portland'),
	('Margaret', 'margaret@example.com', 'Boston'),
	('Dennis', 'dennis@example.com', 'Summit'),
	('Barbara', 'barbara@example.com', 'Cambridge'),
	('Ken', null, 'Berkeley'),
	('Alan', 'alan@example.com', 'Manchester');

create table products(
	id integer primary key,
	name text not null unique,
	price integer not null -- cents
);

insert into products(name, price) values
	('espresso', 250), ('cortado', 300), ('flat white', 350), ('latte', 380), ('mocha', 420);

create table orders(
	id integer primary key,
	customer text not null,
	item text not null,
	qty integer not null default 1,
	total integer not null, -- cents
	status text not null default 'new'
);

with recursive n(i) as (select 1 union all select i + 1 from n where i < 60)
insert into orders(customer, item, qty, total, status)
select c.name, p.name, 1 + i % 3, (1 + i % 3) * p.price,
	case i % 5 when 0 then 'new' when 3 then 'shipped' else 'paid' end
from n
join customers c on c.id = 1 + i % 8
join products p on p.id = 1 + (i * 7 + i / 5) % 5;

create view big_orders as select * from orders where total >= 1000;
