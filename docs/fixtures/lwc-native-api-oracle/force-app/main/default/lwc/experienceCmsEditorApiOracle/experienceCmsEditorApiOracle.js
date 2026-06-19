import { LightningElement } from "lwc";
import * as api from "experience/cmsEditorApi";

export default class Oracle extends LightningElement {
  label = "experience/cmsEditorApi";
  exports = Object.keys(api || {}).join(",");
}
