import { LightningElement } from "lwc";
import * as api from "lightning/platformUtilityBarApi";

export default class Oracle extends LightningElement {
  label = "lightning/platformUtilityBarApi";
  exports = Object.keys(api || {}).join(",");
}
