function lastOrder(orders) {
  return orders[orders.length];
}

function showName(element, name) {
  element.innerHTML = name;
}

function run(request) {
  return eval(request.query.expression);
}

function firstOrder(orders) {
  return orders.length > 0 ? orders[0] : null;
}

module.exports = { lastOrder, showName, run, firstOrder };
