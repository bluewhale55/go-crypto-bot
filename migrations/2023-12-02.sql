create table `bots`
(
    id   int unsigned auto_increment primary key,
    uuid CHAR(36) not null
);
INSERT INTO bots SET uuid = '5b51a35f-76a6-4747-8461-850fff9f7c18'
ALTER TABLE orders ADD column bot_id int unsigned default null
UPDATE orders set bot_id = 1 WHERE id > 0
ALTER TABLE orders change column bot_id bot_id int unsigned not null
ALTER TABLE orders add constraint order_bot_id_fk foreign key (bot_id) references `bots` (id)

ALTER TABLE trade_limit ADD column bot_id int unsigned default null
UPDATE trade_limit set bot_id = 1 WHERE id > 0
ALTER TABLE trade_limit change column bot_id bot_id int unsigned not null
ALTER TABLE trade_limit add constraint trade_limit_bot_id_fk foreign key (bot_id) references `bots` (id)
