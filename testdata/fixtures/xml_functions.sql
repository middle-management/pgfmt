-- XMLELEMENT
SELECT xmlelement(name foo, 'bar');

-- XMLFOREST
SELECT xmlforest(name AS item_name, price AS item_price) FROM products;

-- XMLCONCAT
SELECT xmlconcat('<a/>', '<b/>');

-- XMLPI
SELECT xmlpi(name php, 'echo "hello";');
