-- XMLELEMENT
SELECT xmlelement(name foo, 'bar');

-- XMLFOREST
SELECT xmlforest(name AS item_name, price AS item_price) FROM products;

-- XMLCONCAT
SELECT xmlconcat('<a/>', '<b/>');

-- XMLPI
SELECT xmlpi(name php, 'echo "hello";');

-- XMLSERIALIZE
SELECT XMLSERIALIZE(CONTENT doc AS text) FROM docs;

SELECT XMLSERIALIZE(DOCUMENT doc AS varchar(100)) FROM docs;

SELECT XMLSERIALIZE(CONTENT doc AS text INDENT) FROM docs;
