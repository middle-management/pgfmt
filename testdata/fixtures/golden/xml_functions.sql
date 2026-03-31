-- XMLELEMENT
SELECT
	XMLELEMENT(NAME foo, 'bar');

-- XMLFOREST
SELECT
	XMLFOREST(name AS item_name, price AS item_price)
FROM
	products;

-- XMLCONCAT
SELECT
	XMLCONCAT('<a/>', '<b/>');

-- XMLPI
SELECT
	XMLPI(NAME php, 'echo "hello";');

