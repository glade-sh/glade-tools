"""Behavior checks for the real UI script with a small, in-memory DOM boundary."""

import subprocess
import unittest
from pathlib import Path


APP_JS = Path(__file__).parents[1] / "static" / "app.js"

# No browser, network, or corpus data is involved. The application still registers
# its own event handlers and runs its own rendering and state transitions.
HARNESS = r"""
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const handlers = {};
const errorBox = () => ({hidden:true,textContent:''});
function settingsForm(values, dirty=false) {
  const elements = Object.entries(values).map(([name,value]) => ({name,value}));
  elements.namedItem = name => elements.find(item => item.name === name);
  const form = {
    dataset:{form:'settings',dirty:String(dirty)}, elements,
    error:errorBox(), submit:{disabled:false},
    closest: selector => selector === 'dialog' ? null : form,
    querySelector(selector) {
      if (selector.includes('submit')) return this.submit;
      if (selector.includes('form-error')) return this.error;
      return null;
    },
  };
  return form;
}
let currentForm = null, replacementForm = null;
let currentElements = {}, replacementElements = null;
const nodes = {
  '#repo-count':{}, '#candidate-count':{}, '#activity-count':{},
  '#modal':{open:false,dataset:{},close(){this.open=false;}},
  '#detail':{open:false,addEventListener(){},close(){this.open=false;}},
  '#toast':{classList:{toggle(){}},hidden:true},
};
let renderedHTML = '';
nodes['#main'] = {
  get innerHTML() { return renderedHTML; },
  set innerHTML(value) {
    renderedHTML=value;
    if (replacementForm) currentForm=replacementForm;
    if (replacementElements) currentElements=replacementElements;
  },
};
const document = {
  activeElement:null,
  querySelector(selector) {
    if (selector.includes('form') && selector.includes('settings')) return currentForm;
    return currentElements[selector] || nodes[selector] || null;
  },
  querySelectorAll(selector){return currentElements[selector] || [];},
  getElementById(id){return currentElements['#'+id] || null;},
  addEventListener(type, handler){(handlers[type] ||= []).push(handler);},
};
const context = vm.createContext({
  document, window:{addEventListener(){},scrollTo(){}}, location:{hash:''},
  // Keep startup loading pending; each test explicitly controls state and API.
  fetch:() => new Promise(() => {}),
  setTimeout:() => 1, clearTimeout(){},
  URL, URLSearchParams,
  FormData:class {constructor(form){this.form=form;} *[Symbol.iterator](){
    for(const item of this.form.elements) yield [item.name,item.value];
  }},
  confirm:() => true, assert, handlers, nodes, settingsForm,
  setForms(oldForm,newForm){currentForm=oldForm;replacementForm=newForm;},
  setRenderNodes(oldNodes,newNodes){currentElements=oldNodes;replacementElements=newNodes;},
  getCurrentForm(){return currentForm;},
});
vm.runInContext(fs.readFileSync(process.argv[1],'utf8'),context,{filename:'app.js'});
(async () => {
  const test=fs.readFileSync(0,'utf8');
  await vm.runInContext('(async () => {'+test+'\n})()',context,{filename:'ui-behavior-test.js'});
})().catch(error => {console.error(error);process.exitCode=1;});
"""


class UIBehaviorTests(unittest.TestCase):
    def run_ui(self, script):
        result = subprocess.run(
            ["node", "-e", HARNESS, str(APP_JS)],
            input=script, text=True, capture_output=True, timeout=15,
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_inline_settings_submit_reaches_api_without_modal_error_element(self):
        self.run_ui(r"""
          const form=settingsForm({tiers_file:'/synthetic/tiers.json',
            glade_binary:'/synthetic/glade',glade_sha:'a'.repeat(40),
            runner_binary:'/synthetic/runner',default_host:'worker'},true);
          setForms(form,null);
          let submitted=null;
          mutate=async(path,data)=>{submitted={path,data};return {};};
          await handlers.submit[0]({target:{closest:()=>form},preventDefault(){}});
          assert.equal(submitted?.path,'/api/settings');
          assert.equal(submitted.data.glade_binary,'/synthetic/glade');
          assert.equal(form.error.hidden,true);
        """)

    def test_inline_settings_failure_is_shown_in_the_form(self):
        self.run_ui(r"""
          const form=settingsForm({tiers_file:'/synthetic/tiers.json'},true);
          setForms(form,null);
          mutate=async()=>{throw new Error('The saved settings changed.');};
          await handlers.submit[0]({target:{closest:()=>form},preventDefault(){}});
          assert.equal(form.error.hidden,false);
          assert.equal(form.error.textContent,'The saved settings changed.');
          assert.equal(form.submit.disabled,false);
        """)

    def test_poll_render_preserves_unsaved_settings_fields(self):
        self.run_ui(r"""
          view.tab='settings';
          const draft=settingsForm({glade_binary:'/edited/build',default_host:'worker'},true);
          const fresh=settingsForm({glade_binary:'/saved/build',default_host:'old-worker'});
          setForms(draft,fresh);
          render();
          assert.equal(getCurrentForm(),fresh);
          assert.equal(fresh.elements.namedItem('glade_binary').value,'/edited/build');
          assert.equal(fresh.elements.namedItem('default_host').value,'worker');
          assert.equal(fresh.dataset.dirty,'true');
        """)

    def test_attention_shortcut_clears_unrelated_repository_filters(self):
        self.run_ui(r"""
          Object.assign(view,{search:'old search',host:'old-host',repoStatus:'archived',repoPage:3});
          await handlers.click[0]({target:{closest:()=>({dataset:{action:'attention',value:'never-run'}})}});
          assert.equal(view.search,'');
          assert.equal(view.host,'');
          assert.equal(view.repoStatus,'active');
          assert.equal(view.upstream,'never-run');
          assert.equal(view.repoPage,0);
          assert.equal(location.hash,'repositories');
        """)

    def test_gate_shortcut_clears_unrelated_history_filters(self):
        self.run_ui(r"""
          Object.assign(view,{historyRepo:'old-repository',historyStatus:'failed',historyPhase:'check',historyPage:3});
          await handlers.click[0]({target:{closest:()=>({dataset:{action:'gate-history',value:'synthetic-gate'}})}});
          assert.equal(view.historyRepo,'');
          assert.equal(view.historyStatus,'');
          assert.equal(view.historySearch,'synthetic-gate');
          assert.equal(view.historyPhase,'test');
          assert.equal(view.historyPage,0);
          assert.equal(location.hash,'history');
        """)

    def test_all_history_shortcut_restores_default_history_filters(self):
        self.run_ui(r"""
          Object.assign(view,{historyRepo:'old-repository',historyStatus:'failed',
            historySearch:'old-gate',historyPhase:'check',historyPage:3});
          await handlers.click[0]({target:{closest:()=>({dataset:{action:'all-history'}})}});
          assert.equal(view.historyRepo,'');
          assert.equal(view.historyStatus,'');
          assert.equal(view.historySearch,'');
          assert.equal(view.historyPhase,'test');
          assert.equal(view.historyPage,0);
          assert.equal(location.hash,'history');
        """)

    def test_poll_render_preserves_group_card_and_board_scroll_positions(self):
        self.run_ui(r"""
          view.tab='groups';
          const column=(label,scrollTop)=>({
            cards:{scrollTop},
            getAttribute(name){return name==='aria-labelledby'?label:null;},
            querySelector(selector){return selector==='.group-cards'?this.cards:null;},
          });
          const oldA=column('group-A-title',155),oldB=column('group-B-title',275);
          const newA=column('group-A-title',0),newB=column('group-B-title',0);
          const oldBoard={scrollLeft:210},newBoard={scrollLeft:0};
          setRenderNodes({'.group-column':[oldA,oldB],'.group-board':oldBoard},
            {'.group-column':[newB,newA],'.group-board':newBoard});
          render();
          assert.equal(newA.cards.scrollTop,155);
          assert.equal(newB.cards.scrollTop,275);
          assert.equal(newBoard.scrollLeft,210);
        """)

    def test_navigation_resets_window_scroll_only_when_route_changes(self):
        self.run_ui(r"""
          view.tab='groups';
          location.hash='#repositories';
          const scrollCalls=[];
          window.scrollTo=(...args)=>scrollCalls.push(args);
          navigate();
          assert.equal(view.tab,'repositories');
          assert.equal(scrollCalls.length,1);
          assert.equal(scrollCalls[0][0],0);
          assert.equal(scrollCalls[0][1],0);
          render();
          navigate();
          assert.equal(scrollCalls.length,1,'Same-page renders must retain reading position');
        """)

    def test_successful_bulk_test_renders_the_cleared_selection(self):
        self.run_ui(r"""
          selectedRepos.add('synthetic-repository');
          let submitted=null,renderedSelection=null;
          render=()=>{renderedSelection=Array.from(selectedRepos);};
          mutate=async(path,data)=>{submitted={path,data};render();return {job:'synthetic-job'};};
          await handlers.click[0]({target:{closest:()=>({dataset:{action:'test-selected'}})}});
          assert.equal(submitted.path,'/api/test-many');
          assert.equal(submitted.data.repo_ids[0],'synthetic-repository');
          assert.equal(selectedRepos.size,0);
          assert.equal(renderedSelection.length,0);
        """)

    def test_overview_prioritizes_active_jobs_over_older_failures(self):
        self.run_ui(r"""
          state.jobs=Array.from({length:6},(_,index)=>({id:`failed-${index}`,
            kind:`Failure ${index}`,status:'failed',message:'Synthetic failure',
            started_at:`2026-11-0${index+1}T10:00:00Z`,
            finished_at:`2026-11-0${index+1}T10:01:00Z`}));
          state.jobs.push({id:'active',kind:'Active operation',status:'running',
            message:'Synthetic active job',started_at:'2026-11-09T10:00:00Z'});
          const html=renderOverview();
          assert.ok(html.includes('Active operation'),'Active job must be visible');
          assert.ok(html.indexOf('Active operation')<html.indexOf('Failure 5'),
            'Active work must precede failed jobs');
          assert.ok(!html.includes('Failure 0'),'Old failures must not crowd out active work');
        """)


if __name__ == "__main__":
    unittest.main()
