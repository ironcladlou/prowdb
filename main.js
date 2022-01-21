const sqlPromise = initSqlJs({
  locateFile: file => `https://cdnjs.cloudflare.com/ajax/libs/sql.js/1.6.1/${file}`
});
const dataPromise = fetch("/prow.db").then(res => res.arrayBuffer());
const [SQL, buf] = await Promise.all([sqlPromise, dataPromise])
const db = new SQL.Database(new Uint8Array(buf));

var execBtn = document.getElementById("execute");
var outputElm = document.getElementById('output');
var errorElm = document.getElementById('error');
var commandsElm = document.getElementById('commands');

function error(e) {
  console.log(e);
  errorElm.style.height = '2em';
  errorElm.textContent = e.message;
}

function noerror() {
  errorElm.style.height = '0';
}

// Run a command in the database
function execute(commands) {
  const results = db.exec(commands);
  if (!results) {
    error({message: event.data.error});
    return;
  }
  outputElm.innerHTML = "";
  for (var i = 0; i < results.length; i++) {
    outputElm.appendChild(tableCreate(results[i].columns, results[i].values));
  }
}

// Create an HTML table
var tableCreate = function () {
  function valconcat(vals, tagName) {
    if (vals.length === 0) return '';
    var open = '<' + tagName + '>', close = '</' + tagName + '>';
    return open + vals.join(close + open) + close;
  }
  return function (columns, values) {
    var tbl = document.createElement('table');
    tbl.setAttribute("class", "table");
    var html = '<thead>' + valconcat(columns, 'th') + '</thead>';
    var rows = values.map(function (v) { return valconcat(v, 'td'); });
    html += '<tbody>' + valconcat(rows, 'tr') + '</tbody>';
    tbl.innerHTML = html;
    return tbl;
  }
}();

// Execute the commands when the button is clicked
function execEditorContents() {
  noerror()
  execute(commandsElm.value + ';');
}

export { execEditorContents }
